/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mutation

import (
	"context"
	"encoding/json"
	"net/http"

	sc "github.com/kubernetes-incubator/service-catalog/pkg/apis/servicecatalog/v1beta1"
	scfeatures "github.com/kubernetes-incubator/service-catalog/pkg/features"
	"github.com/kubernetes-incubator/service-catalog/pkg/webhookutil"

	admissionTypes "k8s.io/api/admission/v1beta1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// CreateUpdateHandler handles ServiceBinding
type CreateUpdateHandler struct {
	decoder *admission.Decoder
	UUID    webhookutil.UUIDGenerator
}

var _ admission.Handler = &CreateUpdateHandler{}

// Handle handles admission requests.
func (h *CreateUpdateHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	traced := webhookutil.NewTracedLogger(req.UID)
	traced.Infof("Start handling operation: %s for %s: %q", req.Operation, req.Kind.Kind, req.Name)

	sb := &sc.ServiceBinding{}
	if err := webhookutil.MatchKinds(sb, req.Kind); err != nil {
		traced.Errorf("Error matching kinds: %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := h.decoder.Decode(req, sb); err != nil {
		traced.Errorf("Could not decode request object: %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	mutated := sb.DeepCopy()
	switch req.Operation {
	case admissionTypes.Create:
		h.mutateOnCreate(ctx, req, mutated)
	case admissionTypes.Update:
		h.mutateOnUpdate(ctx, mutated)
	default:
		traced.Infof("ServiceBinding mutation wehbook does not support action %q", req.Operation)
		return admission.Allowed("action not taken")
	}

	rawMutated, err := json.Marshal(mutated)
	if err != nil {
		traced.Errorf("Error marshaling mutated object: %v", err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	traced.Infof("Completed successfully operation: %s for %s: %q", req.Operation, req.Kind.Kind, req.Name)
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, rawMutated)
}

var _ admission.DecoderInjector = &CreateUpdateHandler{}

// InjectDecoder injects the decoder
func (h *CreateUpdateHandler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}

func (h *CreateUpdateHandler) mutateOnCreate(ctx context.Context, req admission.Request, binding *sc.ServiceBinding) {
	binding.Finalizers = []string{sc.FinalizerServiceCatalog}

	if binding.Spec.ExternalID == "" {
		binding.Spec.ExternalID = string(h.UUID.New())
	}

	if binding.Spec.SecretName == "" {
		binding.Spec.SecretName = binding.Name
	}

	if utilfeature.DefaultFeatureGate.Enabled(scfeatures.OriginatingIdentity) {
		setServiceBindingUserInfo(req, binding)
	}

	// TODO: cannot be modified on webhook side, need to moved directly to controller
	//binding.Status = sc.ServiceBindingStatus{
	//	Conditions: []sc.ServiceBindingCondition{},
	//	UnbindStatus: sc.ServiceBindingUnbindStatusNotRequired,
	//}
}

func (h *CreateUpdateHandler) mutateOnUpdate(ctx context.Context, obj *sc.ServiceBinding) {
	// TODO: implement logic from pkg/registry/servicecatalog/binding/strategy.go
}

// setServiceBindingUserInfo injects user.Info from the request context
func setServiceBindingUserInfo(req admission.Request, binding *sc.ServiceBinding) {
	user := req.UserInfo

	binding.Spec.UserInfo = &sc.UserInfo{
		Username: user.Username,
		UID:      user.UID,
		Groups:   user.Groups,
	}
	if extra := user.Extra; len(extra) > 0 {
		binding.Spec.UserInfo.Extra = map[string]sc.ExtraValue{}
		for k, v := range extra {
			binding.Spec.UserInfo.Extra[k] = sc.ExtraValue(v)
		}
	}
}
