package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/aisuggest"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// AI-drafted test cases (QĐ-15, QĐ-18). An organization's admins and owners
// pick the provider, the model and whose key pays — their own for any
// provider offered, or the server's LLM_API_KEY (for LLM_PROVIDER) when the
// operator set one. Testers (testmgmt.write) ask for drafts in a project;
// anyone in it reads them. A key is never returned — only an own key's last
// four characters.
//
//	GET    /v1/organizations/{organizationId}/ai-settings
//	POST   /v1/organizations/{organizationId}/ai-settings {provider?, model, keySource: organization|server, apiKey?}  (save; upsert)
//	DELETE /v1/organizations/{organizationId}/ai-settings
//	GET    /v1/projects/{projectId}/ai-status
//	POST   /v1/projects/{projectId}/case-suggestions {testType, environmentId?, url?, context?, count?} → 202
//	GET    /v1/projects/{projectId}/case-suggestions/{suggestionId}

func (d *dashboard) aiRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("GET /v1/organizations/{organizationId}/ai-settings", d.aiSettings)
	handle("POST /v1/organizations/{organizationId}/ai-settings", d.saveAISettings)
	handle("DELETE /v1/organizations/{organizationId}/ai-settings", d.deleteAISettings)
	handle("GET /v1/projects/{projectId}/ai-status", d.aiStatus)
	handle("POST /v1/projects/{projectId}/case-suggestions", d.createSuggestion)
	handle("GET /v1/projects/{projectId}/case-suggestions/{suggestionId}", d.suggestion)
}

type aiModelDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Note  string `json:"note"`
}

// aiProviderDTO is one provider an organization may choose: how its keys
// look and where they are made, the models suggested (for Anthropic, the
// only ones), and whether another model id may be typed.
type aiProviderDTO struct {
	ID           string       `json:"id"`
	Label        string       `json:"label"`
	Console      string       `json:"console"`
	KeyHint      string       `json:"keyHint"`
	KeyPrefixes  []string     `json:"keyPrefixes"`
	Models       []aiModelDTO `json:"models"`
	DefaultModel string       `json:"defaultModel"`
	AnyModel     bool         `json:"anyModel"`
}

type aiAccessDTO struct {
	Available bool   `json:"available"`
	Source    string `json:"source"` // organization | server | ""
	Provider  string `json:"provider"`
	Model     string `json:"model"`
}

type aiOrgKeyDTO struct {
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	KeySource string    `json:"keySource"` // organization | server
	KeyHint   string    `json:"keyHint"`   // "" with the server's key
	UpdatedBy string    `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func modelsOut(ms []llm.Model) []aiModelDTO {
	out := make([]aiModelDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, aiModelDTO{ID: m.ID, Label: m.Label, Note: m.Note})
	}
	return out
}

// aiProviders is what this server offers: platform/llm's list, and the
// operator's own endpoint where LLM_PROVIDER names one (its model is
// LLM_MODEL).
func (d *dashboard) aiProviders() []aiProviderDTO {
	list := llm.Providers()
	if d.ai.ServerProvider() == llm.Custom {
		p, _ := llm.Find(llm.Custom)
		p.DefaultModel = d.ai.ServerModel()
		p.Models = []llm.Model{{ID: p.DefaultModel, Label: p.DefaultModel, Note: "The model this server's endpoint is set up with (LLM_MODEL)."}}
		list = append(list, p)
	}
	out := make([]aiProviderDTO, 0, len(list))
	for _, p := range list {
		prefixes := append([]string{}, p.KeyPrefixes...)
		out = append(out, aiProviderDTO{ID: p.ID, Label: p.Label, Console: p.Console, KeyHint: p.KeyHint, KeyPrefixes: prefixes,
			Models: modelsOut(p.Models), DefaultModel: p.DefaultModel, AnyModel: p.AnyModel})
	}
	return out
}

// aiOrg resolves and authorizes an organization for its AI settings:
// owners and admins (member.manage at the organization).
func (d *dashboard) aiOrg(w http.ResponseWriter, r *http.Request) (authz.Principal, string, bool) {
	p, ok := d.user(w, r)
	if !ok {
		return p, "", false
	}
	org, err := d.tenancy.GetOrganization(r.Context(), tenancy.OrganizationID(r.PathValue("organizationId")))
	if err != nil {
		d.nodeErr(w, r, err, "ORGANIZATION_NOT_FOUND", "organization not found")
		return p, "", false
	}
	if !d.authorizeNode(w, r, p, authz.Resource{OrgID: string(org.ID)}, authz.ActionMemberManage, "ORGANIZATION_NOT_FOUND", "organization not found") {
		return p, "", false
	}
	return p, string(org.ID), true
}

func (d *dashboard) writeAISettings(w http.ResponseWriter, r *http.Request, orgID string) {
	st, own, err := d.ai.Settings(r.Context(), orgID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	access, err := d.ai.Access(r.Context(), orgID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	out := map[string]any{
		"providers": d.aiProviders(),
		// models and defaultModel are Anthropic's list and the server's model,
		// as before providers (QĐ-15); a dashboard reads providers now.
		"models":       modelsOut(llm.Models),
		"server":       map[string]any{"available": d.llmServerKey, "provider": d.ai.ServerProvider(), "model": d.ai.ServerModel()},
		"defaultModel": d.ai.ServerModel(),
		"organization": nil,
		"effective":    aiAccessDTO{Available: access.Available, Source: access.Source, Provider: access.Provider, Model: access.Model},
	}
	if own {
		source := aisuggest.SourceServer
		if st.OwnKey {
			source = aisuggest.SourceOrganization
		}
		out["organization"] = aiOrgKeyDTO{Provider: st.Provider, Model: st.Model, KeySource: source, KeyHint: st.KeyHint, UpdatedBy: st.UpdatedBy, UpdatedAt: st.UpdatedAt}
	}
	writeJSON(w, http.StatusOK, out)
}

// invalidField is the request field an aisuggest.ErrInvalid names first
// ("provider", "model", "keySource", "apiKey").
func invalidField(msg string) string {
	for _, f := range []string{"provider", "model", "keySource"} {
		if strings.HasPrefix(msg, f) {
			return f
		}
	}
	return "apiKey"
}

func (d *dashboard) aiSettings(w http.ResponseWriter, r *http.Request) {
	if _, orgID, ok := d.aiOrg(w, r); ok {
		d.writeAISettings(w, r, orgID)
	}
}

func (d *dashboard) saveAISettings(w http.ResponseWriter, r *http.Request) {
	p, orgID, ok := d.aiOrg(w, r)
	if !ok {
		return
	}
	var req struct {
		Provider  string `json:"provider"`
		Model     string `json:"model"`
		KeySource string `json:"keySource"`
		APIKey    string `json:"apiKey"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	// No model: the provider's suggestion (Anthropic's when no provider is
	// named either, as before providers); the operator's endpoint's is
	// LLM_MODEL. No provider: the one that offers the model by name.
	if req.Model == "" {
		p, _ := llm.Find(req.Provider)
		req.Model = p.DefaultModel
		if p.ID == llm.Custom {
			req.Model = d.ai.ServerModel()
		}
	}
	if req.Provider == "" {
		req.Provider = llm.ProviderOf(req.Model)
	}
	label := llm.Label(req.Provider)
	if req.KeySource == "" {
		req.KeySource = aisuggest.SourceOrganization
	}
	if req.KeySource != aisuggest.SourceOrganization && req.KeySource != aisuggest.SourceServer {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "keySource is organization or server", map[string]any{"field": "keySource"})
		return
	}
	if req.KeySource == aisuggest.SourceServer && req.APIKey != "" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "an apiKey goes with keySource organization", map[string]any{"field": "apiKey"})
		return
	}
	actor := d.actor(r, p)
	st, err := d.ai.SaveSettings(r.Context(), orgID, aisuggest.SaveInput{Provider: req.Provider, Model: req.Model,
		OwnKey: req.KeySource == aisuggest.SourceOrganization, APIKey: req.APIKey, Actor: actor})
	var unknown *llm.ModelError
	switch {
	case errors.Is(err, aisuggest.ErrInvalid):
		msg := strings.TrimPrefix(err.Error(), "aisuggest: invalid input: ")
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", msg, map[string]any{"field": invalidField(msg)})
		return
	case errors.Is(err, llm.ErrKey) && req.KeySource == aisuggest.SourceServer:
		httpx.WriteError(w, http.StatusBadRequest, "AI_SERVER_KEY_REJECTED", label+" rejected the server's key (LLM_API_KEY); tell the operator, or use your own key", map[string]any{"field": "keySource", "provider": req.Provider})
		return
	case errors.Is(err, llm.ErrKey):
		httpx.WriteError(w, http.StatusBadRequest, "AI_KEY_REJECTED", label+" rejected this API key", map[string]any{"field": "apiKey", "provider": req.Provider})
		return
	case errors.Is(err, llm.ErrQuota):
		httpx.WriteError(w, http.StatusBadRequest, "AI_KEY_NO_CREDIT", "the "+label+" account behind this key is out of credit or over its spending limit", map[string]any{"field": "apiKey", "provider": req.Provider})
		return
	case errors.As(err, &unknown) && len(unknown.Available) > 0:
		httpx.WriteError(w, http.StatusBadRequest, "AI_MODEL_UNAVAILABLE", "this key cannot use "+req.Model+"; models it can use include "+strings.Join(unknown.Available, ", "),
			map[string]any{"field": "model", "provider": req.Provider, "available": unknown.Available})
		return
	case errors.Is(err, llm.ErrModel):
		httpx.WriteError(w, http.StatusBadRequest, "AI_MODEL_UNAVAILABLE", "this key cannot use "+req.Model, map[string]any{"field": "model", "provider": req.Provider})
		return
	case errors.Is(err, llm.ErrRateLimited), errors.Is(err, llm.ErrUnavailable):
		httpx.WriteError(w, http.StatusBadGateway, "AI_UNREACHABLE", label+" could not be reached to check the key; try again", map[string]any{"provider": req.Provider})
		return
	case err != nil:
		d.internal(w, r, err)
		return
	}
	if err := d.authz.AuditOrganization(r.Context(), "organization.ai_settings", orgID, actor,
		map[string]any{"provider": st.Provider, "model": st.Model, "keySource": req.KeySource, "keyHint": st.KeyHint, "keyChanged": req.APIKey != ""}); err != nil {
		d.internal(w, r, err)
		return
	}
	d.writeAISettings(w, r, orgID)
}

func (d *dashboard) deleteAISettings(w http.ResponseWriter, r *http.Request) {
	p, orgID, ok := d.aiOrg(w, r)
	if !ok {
		return
	}
	removed, err := d.ai.DeleteSettings(r.Context(), orgID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	if removed {
		if err := d.authz.AuditOrganization(r.Context(), "organization.ai_settings", orgID, d.actor(r, p), map[string]any{"removed": true}); err != nil {
			d.internal(w, r, err)
			return
		}
	}
	d.writeAISettings(w, r, orgID)
}

func (d *dashboard) aiStatus(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	access, err := d.ai.Access(r.Context(), string(scope.OrgID))
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"available": access.Available, "source": access.Source, "provider": access.Provider, "model": access.Model,
		"readsPages": d.urlChecksOn, // the checker reads the site's outline only with URLCHECK=on
	})
}

type suggestionDTO struct {
	ID            string            `json:"id"`
	EnvironmentID string            `json:"environmentId"`
	TestType      string            `json:"testType"`
	URL           string            `json:"url"`
	Context       string            `json:"context"`
	Count         int               `json:"count"`
	Status        string            `json:"status"`
	Drafts        []aisuggest.Draft `json:"drafts"`
	PageRead      bool              `json:"pageRead"`
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	KeySource     string            `json:"keySource"`
	Error         string            `json:"error"`
	RequestedBy   string            `json:"requestedBy"`
	CreatedAt     time.Time         `json:"createdAt"`
	FinishedAt    *time.Time        `json:"finishedAt"`
}

func suggestionOut(s aisuggest.Suggestion) suggestionDTO {
	out := suggestionDTO{ID: s.ID, EnvironmentID: s.EnvironmentID, TestType: s.TestType, URL: s.URL, Context: s.Context, Count: s.Count,
		Status: s.Status, Drafts: s.Drafts, PageRead: s.PageRead, Provider: s.Provider, Model: s.Model, KeySource: s.KeySource, Error: s.Error,
		RequestedBy: s.RequestedBy, CreatedAt: s.CreatedAt}
	for i := range out.Drafts {
		if out.Drafts[i].Steps == nil {
			out.Drafts[i].Steps = []aisuggest.Step{}
		}
	}
	if !s.FinishedAt.IsZero() {
		at := s.FinishedAt
		out.FinishedAt = &at
	}
	return out
}

func (d *dashboard) createSuggestion(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	scope, ok := d.project(w, r, p, authz.ActionTestmgmtWrite)
	if !ok {
		return
	}
	var req struct {
		TestType      string `json:"testType"`
		EnvironmentID string `json:"environmentId"`
		URL           string `json:"url"`
		Context       string `json:"context"`
		Count         int    `json:"count"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	// An environment alone means its address (like a URL check).
	if req.EnvironmentID != "" {
		envs, ok := d.planEnvironment(w, r, string(scope.ProjectID), req.EnvironmentID)
		if !ok {
			return
		}
		if strings.TrimSpace(req.URL) == "" {
			req.URL = envs[req.EnvironmentID].BaseURL
		}
	}
	s, err := d.ai.Request(r.Context(), aisuggest.NewSuggestion{ProjectID: string(scope.ProjectID), OrgID: string(scope.OrgID),
		EnvironmentID: req.EnvironmentID, TestType: req.TestType, URL: req.URL, Context: req.Context, Count: req.Count, Actor: d.actor(r, p)})
	switch {
	case errors.Is(err, aisuggest.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", strings.TrimPrefix(err.Error(), "aisuggest: invalid input: "), nil)
	case errors.Is(err, aisuggest.ErrNotConfigured):
		httpx.WriteError(w, http.StatusConflict, "AI_NOT_CONFIGURED", "no AI key: an organization admin adds one under Team → AI settings, or the operator sets LLM_API_KEY", nil)
	case errors.Is(err, aisuggest.ErrBusy):
		httpx.WriteError(w, http.StatusTooManyRequests, "TOO_MANY_SUGGESTIONS", "this project has suggestions waiting or reached today's limit",
			map[string]any{"maxWaiting": aisuggest.MaxQueued, "perDay": aisuggest.DailyLimit})
	case err != nil:
		d.internal(w, r, err)
	default:
		writeJSON(w, http.StatusAccepted, suggestionOut(s))
	}
}

func (d *dashboard) suggestion(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	s, err := d.ai.Get(r.Context(), string(scope.ProjectID), r.PathValue("suggestionId"))
	if errors.Is(err, aisuggest.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "suggestion not found", nil)
		return
	}
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, suggestionOut(s))
}
