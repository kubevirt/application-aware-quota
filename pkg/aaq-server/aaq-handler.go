package aaq_server

import (
	"bytes"
	"encoding/json"
	"fmt"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	handlerv1 "kubevirt.io/applications-aware-quota/pkg/aaq-server/handler"
	"net/http"
)

type AaqServerHandler struct {
	aaqCli kubernetes.Interface
	aaqNS  string
}

func NewAaqServerHandler(aaqNS string, aaqCli kubernetes.Interface) *AaqServerHandler {
	return &AaqServerHandler{aaqCli, aaqNS}
}

func (ash *AaqServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	in, err := parseRequest(*r)
	if err != nil {
		klog.Error(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	handler := handlerv1.NewHandler(in.Request, ash.aaqCli, ash.aaqNS)

	out, err := handler.Handle()
	if err != nil {
		e := fmt.Sprintf("could not generate admission response: %v", err)
		klog.Error(err.Error())
		http.Error(w, e, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	jout, err := json.Marshal(out)
	if err != nil {
		e := fmt.Sprintf("could not parse admission response: %v", err)
		klog.Error(err.Error())
		http.Error(w, e, http.StatusInternalServerError)
		return
	}

	_, err = fmt.Fprintf(w, "%s", jout)
	if err != nil {
		klog.Error(err.Error())
	}

}

// parseRequest extracts an AdmissionReview from an http.Request if possible
func parseRequest(r http.Request) (*admissionv1.AdmissionReview, error) {
	if r.Header.Get("Content-Type") != "application/json" {
		return nil, fmt.Errorf("Content-Type: %q should be %q",
			r.Header.Get("Content-Type"), "application/json")
	}

	bodybuf := new(bytes.Buffer)
	_, err := bodybuf.ReadFrom(r.Body)
	if err != nil {
		return nil, err
	}
	body := bodybuf.Bytes()

	if len(body) == 0 {
		return nil, fmt.Errorf("admission request body is empty")
	}

	var a admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("could not parse admission review request: %v", err)
	}

	if a.Request == nil {
		return nil, fmt.Errorf("admission review can't be used: request field is nil")
	}

	return &a, nil
}
