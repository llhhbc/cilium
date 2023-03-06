package main


import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/mattbaird/jsonpatch"
	admv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func ServerInject(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	if len(body) == 0 {
		klog.Errorf("no body found")
		http.Error(w, "no body found", http.StatusBadRequest)
		return
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		klog.Errorf("contentType=%s, expect application/json", contentType)
		http.Error(w, "invalid Content-Type, want `application/json`", http.StatusUnsupportedMediaType)
		return
	}

	var reviewResponse *admv1.AdmissionResponse
	ar := admv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		klog.Errorf("Could not decode body: %v", err)
		reviewResponse = toAdmissionResponse(err)
	} else {
		switch ar.Request.Resource.Resource {
		case "pods":
			reviewResponse = injectPod(&ar)
		default:
			reviewResponse = toAdmissionResponse(fmt.Errorf("get unsupported type %s. ", ar.Request.Resource.Resource))
		}
	}

	response := admv1.AdmissionReview{
		TypeMeta: ar.TypeMeta,
	}
	if reviewResponse != nil {
		response.Response = reviewResponse
		if ar.Request != nil {
			response.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(response)
	if err != nil {
		klog.Errorf("Could not encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
	}
	if _, err := w.Write(resp); err != nil {
		klog.Errorf("Could not write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

var pt = admv1.PatchTypeJSONPatch
var allow = &admv1.AdmissionResponse{
	Allowed: true,
}

func toAdmissionResponse(err error) *admv1.AdmissionResponse {
	return &admv1.AdmissionResponse{Result: &metav1.Status{Message: err.Error()}}
}

func injectPod(ar *admv1.AdmissionReview) *admv1.AdmissionResponse {
	req := ar.Request
	var pod corev1.Pod
	if req.Resource.Resource != "pods" {
		klog.V(1).Infof("get unsupported resources %s. ", req.Resource.Resource)
		return allow
	}

	switch req.Operation {
	case admv1.Create:
	default:
		return allow
	}

	err := json.Unmarshal(req.Object.Raw, &pod)
	if err != nil {
		klog.Errorf("Could not unmarshal raw object: %v %s", err,
			string(req.Object.Raw))
		return toAdmissionResponse(err)
	}

	from, _ := json.Marshal(pod)
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string, 0)
	}

	//if pod.Annotations[CiliumIPAMPodAnnotation] != "" {
	//	return allow
	//}
	//
	//pod.Annotations[CiliumIPAMPodAnnotation] = "true"

	to, _ := json.Marshal(pod)
	patch, err := jsonpatch.CreatePatch(from, to)
	if err != nil {
		klog.Errorf("create patch failed %v. ", err)
		return toAdmissionResponse(err)
	}
	allow.Patch, _ = json.Marshal(patch)
	allow.PatchType = &pt

	return allow
}
