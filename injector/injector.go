package injector

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Mutator struct
type Mutator struct {
	// CoordAddr contains the address of the marblerun coordinator
	CoordAddr   string
	DomainName  string
	SGXResource string
}

// HandleMutate handles mutate requests and injects sgx tolerations into the request
func (m *Mutator) HandleMutate(w http.ResponseWriter, r *http.Request) {
	log.Println("Handling mutate request, injecting sgx tolerations")
	body := checkRequest(w, r)
	if body == nil {
		// Error was already written to w
		return
	}

	// mutate the request and add sgx tolerations to pod
	mutatedBody, err := mutate(body, m.CoordAddr, m.DomainName, m.SGXResource, true)
	if err != nil {
		http.Error(w, "unable to mutate request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(mutatedBody)
}

// HandleMutateNoSgx is called when the sgx injection label is not set
func (m *Mutator) HandleMutateNoSgx(w http.ResponseWriter, r *http.Request) {
	log.Println("Handling mutate request, omitting sgx injection")
	body := checkRequest(w, r)
	if body == nil {
		// Error was already written to w
		return
	}

	// mutate the request and add sgx tolerations to pod
	mutatedBody, err := mutate(body, m.CoordAddr, m.DomainName, m.SGXResource, false)
	if err != nil {
		http.Error(w, "unable to mutate request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(mutatedBody)
}

// mutate handles the creation of json patches for pods
func mutate(body []byte, coordAddr string, domainName string, resourceKey string, injectSgx bool) ([]byte, error) {
	admReviewReq := v1.AdmissionReview{}
	if err := json.Unmarshal(body, &admReviewReq); err != nil {
		log.Println("Unable to mutate request: invalid admission review")
		return nil, errors.New("invalid admission review")
	}

	if admReviewReq.Request == nil {
		log.Println("Unable to mutate request: empty admission review request")
		return nil, errors.New("empty admission request")
	}

	var pod corev1.Pod
	if err := json.Unmarshal(admReviewReq.Request.Object.Raw, &pod); err != nil {
		log.Println("Unable to mutate request: invalid pod")
		return nil, errors.New("invalid pod")
	}

	// admission response
	admReviewResponse := v1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &v1.AdmissionResponse{
			UID: admReviewReq.Request.UID,
		},
	}

	// get marble type from pod labels
	marbleType := pod.Labels["marblerun/marbletype"]
	// allow pod to start if label does not exist, but dont inject any values
	if len(marbleType) == 0 {
		admReviewResponse.Response.Allowed = true
		admReviewResponse.Response.Result = &metav1.Status{
			Status:  "Success",
			Message: "Missing [marblerun/marbletype] label, injection skipped",
		}
		bytes, err := json.Marshal(admReviewResponse)
		if err != nil {
			log.Println("Error: unable to marshal admission response")
			return nil, errors.New("unable to marshal admission response")
		}
		log.Println("Pod is missing [marblerun/marbletype] label, skipping injection")
		return bytes, nil
	}

	pT := v1.PatchTypeJSONPatch
	admReviewResponse.Response.PatchType = &pT

	// get namespace of pod
	namespace := pod.Namespace
	if len(namespace) == 0 {
		namespace = "default"
	}

	newEnvVars := []corev1.EnvVar{
		{
			Name:  "EDG_MARBLE_COORDINATOR_ADDR",
			Value: coordAddr,
		},
		{
			Name:  "EDG_MARBLE_TYPE",
			Value: marbleType,
		},
		{
			Name:  "EDG_MARBLE_DNS_NAMES",
			Value: fmt.Sprintf("%s,%s.%s,%s.%s.svc.%s", marbleType, marbleType, namespace, marbleType, namespace, domainName),
		},
	}

	var patch []map[string]interface{}
	var needNewVolume bool

	// create env variable patches for each container of the pod
	for idx, container := range pod.Spec.Containers {
		if !envIsSet(container.Env, corev1.EnvVar{Name: "EDG_MARBLE_UUID_FILE"}) {
			needNewVolume = true

			newEnvVars = append(newEnvVars, corev1.EnvVar{
				Name:  "EDG_MARBLE_UUID_FILE",
				Value: fmt.Sprintf("/%s-uid/uuid-file", marbleType),
			})

			// If we need to set the uuid env variable we also need to create a volume mount, which the variable points to
			patch = append(patch, createMountPatch(
				len(container.VolumeMounts),
				fmt.Sprintf("/spec/containers/%d/volumeMounts", idx),
				fmt.Sprintf("/%s-uid", marbleType),
				string(admReviewReq.Request.UID),
			))
		}
		patch = append(patch, addEnvVar(container.Env, newEnvVars, fmt.Sprintf("/spec/containers/%d/env", idx))...)

		if injectSgx {
			patch = append(patch, createResourcePatch(container, idx, resourceKey))
		}
	}

	if needNewVolume {
		patch = append(patch, createVolumePatch(len(pod.Spec.Volumes), string(admReviewReq.Request.UID)))
	}

	// add sgx tolerations if enabled
	if injectSgx {
		if len(pod.Spec.Tolerations) <= 0 {
			// create array if this is the first toleration of the pod
			patch = append(patch, map[string]interface{}{
				"op":   "add",
				"path": "/spec/tolerations",
				"value": []corev1.Toleration{
					{
						Key:      resourceKey,
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			})
		} else {
			// append as last element of the tolerations array otherwise
			patch = append(patch, map[string]interface{}{
				"op":   "add",
				"path": "/spec/tolerations/-",
				"value": corev1.Toleration{
					Key:      resourceKey,
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			})
		}
	}

	// convert admission response into bytes and return
	var err error
	admReviewResponse.Response.Patch, err = json.Marshal(patch)
	if err != nil {
		log.Println("Error: unable to marshal json patch")
		return nil, errors.New("unable to marshal json patch")
	}
	admReviewResponse.Response.Allowed = true
	bytes, err := json.Marshal(admReviewResponse)
	if err != nil {
		log.Println("Error: unable to marshal admission response")
		return nil, errors.New("unable to marshal admission response")
	}

	log.Printf("Mutation request for pod of marble type [%s] successful", marbleType)
	return bytes, nil
}

// check if http was POST and not empty
func checkRequest(w http.ResponseWriter, r *http.Request) []byte {
	if r.Method != http.MethodPost {
		http.Error(w, "unable to handle requests other than POST", http.StatusBadRequest)
		return nil
	}

	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		http.Error(w, "wrong application type", http.StatusBadRequest)
		return nil
	}

	body, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, "unable to read request", http.StatusBadRequest)
		return nil
	}

	return body
}

// envIsSet checks if an env variable is already set
func envIsSet(setVars []corev1.EnvVar, testVar corev1.EnvVar) bool {
	if len(setVars) == 0 {
		return false
	}
	for _, setVar := range setVars {
		if setVar.Name == testVar.Name {
			return true
		}
	}
	return false
}

// addEnvVar creates a json patch setting all unset required environment variables
func addEnvVar(setVars, newVars []corev1.EnvVar, basePath string) []map[string]interface{} {
	var envPatch []map[string]interface{}
	first := len(setVars) == 0
	var newValue interface{}
	for _, newVar := range newVars {
		newValue = newVar
		path := basePath
		// if the to be added env variable is the first of the pod we have to create the env field of the spec as an array
		// otherwise we append the env variable the as the last element to the array
		if first {
			first = false
			newValue = []corev1.EnvVar{newVar}
		} else {
			path = path + "/-"
		}
		if !envIsSet(setVars, newVar) {
			envPatch = append(envPatch, map[string]interface{}{
				"op":    "add",
				"path":  path,
				"value": newValue,
			})
		}
	}
	return envPatch
}

// createResourcePatch creates a json patch for sgx resource limits
func createResourcePatch(container corev1.Container, idx int, resourceKey string) map[string]interface{} {
	// first check if neither limits nor requests have been set for the container -> we need to create the complete path
	if len(container.Resources.Limits) <= 0 && len(container.Resources.Requests) <= 0 {
		return map[string]interface{}{
			"op":   "add",
			"path": fmt.Sprintf("/spec/containers/%d/resources", idx),
			"value": map[string]interface{}{
				"limits": map[string]int{
					resourceKey: 10,
				},
			},
		}
	}
	// next check if only requests have been set -> we only need to create the limits path
	if len(container.Resources.Limits) <= 0 {
		return map[string]interface{}{
			"op":   "add",
			"path": fmt.Sprintf("/spec/containers/%d/resources/limits", idx),
			"value": map[string]int{
				resourceKey: 10,
			},
		}
	}

	// default case: both requests and limits have been set -> we can just add a new value
	// replace any "/" in the added key with "~1" so JSONPatch does not interpret it as a path
	newKey := strings.Replace(resourceKey, "/", "~1", -1)
	return map[string]interface{}{
		"op":    "add",
		"path":  fmt.Sprintf("/spec/containers/%d/resources/limits/%s", idx, newKey),
		"value": 10,
	}
}

// createMountPatch creates a json patch to mount a volume on a pod
func createMountPatch(mounts int, path string, mountpath string, uid string) map[string]interface{} {
	val := corev1.VolumeMount{
		Name:      fmt.Sprintf("uuid-file-%s", uid),
		MountPath: mountpath,
	}

	// If no other volumeMounts exist we have to created the first one as an array
	if mounts <= 0 {
		return map[string]interface{}{
			"op":    "add",
			"path":  path,
			"value": []corev1.VolumeMount{val},
		}
	}
	return map[string]interface{}{
		"op":    "add",
		"path":  fmt.Sprintf("%s/-", path),
		"value": val,
	}

}

// createVolumePatch creates a json patch which creates a volume utilising the k8s downward api
func createVolumePatch(volumes int, uid string) map[string]interface{} {
	val := corev1.Volume{
		Name: fmt.Sprintf("uuid-file-%s", uid),
		VolumeSource: corev1.VolumeSource{
			DownwardAPI: &corev1.DownwardAPIVolumeSource{
				Items: []corev1.DownwardAPIVolumeFile{
					{
						Path: "uuid-file",
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.uid",
						},
					},
				},
			},
		},
	}

	// If no other volumes exist we have to created the first one as an array
	if volumes <= 0 {
		return map[string]interface{}{
			"op":    "add",
			"path":  "/spec/volumes",
			"value": []corev1.Volume{val},
		}
	}
	return map[string]interface{}{
		"op":    "add",
		"path":  "/spec/volumes/-",
		"value": val,
	}
}
