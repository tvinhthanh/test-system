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

// AI-drafted test cases (QĐ-15). An organization's admins and owners pick
// the model and whose Anthropic API key pays — their own, or the server's
// LLM_API_KEY when the operator set one. Testers (testmgmt.write) ask for
// drafts in a project; anyone in it reads them. A key is never returned —
// only an own key's last four characters.
//
//	GET    /v1/organizations/{organizationId}/ai-settings
//	POST   /v1/organizations/{organizationId}/ai-settings {model, keySource: organization|server, apiKey?}  (save; upsert)
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

type aiAccessDTO struct {
	Available bool   `json:"available"`
	Source    string `json:"source"` // organization | server | ""
	Model     string `json:"model"`
}

type aiOrgKeyDTO struct {
	Model     string    `json:"model"`
	KeySource string    `json:"keySource"` // organization | server
	KeyHint   string    `json:"keyHint"`   // "" with the server's key
	UpdatedBy string    `json:"updatedBy"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	models := make([]aiModelDTO, 0, len(llm.Models))
	for _, m := range llm.Models {
		models = append(models, aiModelDTO{ID: m.ID, Label: m.Label, Note: m.Note})
	}
	out := map[string]any{
		"models":       models,
		"server":       map[string]any{"available": d.llmServerKey},
		"defaultModel": llm.DefaultModel,
		"organization": nil,
		"effective":    aiAccessDTO{Available: access.Available, Source: access.Source, Model: access.Model},
	}
	if own {
		source := aisuggest.SourceServer
		if st.OwnKey {
			source = aisuggest.SourceOrganization
		}
		out["organization"] = aiOrgKeyDTO{Model: st.Model, KeySource: source, KeyHint: st.KeyHint, UpdatedBy: st.UpdatedBy, UpdatedAt: st.UpdatedAt}
	}
	writeJSON(w, http.StatusOK, out)
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
		Model     string `json:"model"`
		KeySource string `json:"keySource"`
		APIKey    string `json:"apiKey"`
	}
	if !decodeSmall(w, r, &req) {
		return
	}
	if req.Model == "" {
		req.Model = llm.DefaultModel
	}
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
	st, err := d.ai.SaveSettings(r.Context(), orgID, aisuggest.SaveInput{Model: req.Model, OwnKey: req.KeySource == aisuggest.SourceOrganization, APIKey: req.APIKey, Actor: actor})
	switch {
	case errors.Is(err, aisuggest.ErrInvalid):
		field := "apiKey"
		switch {
		case strings.Contains(err.Error(), "model"):
			field = "model"
		case strings.Contains(err.Error(), "keySource"):
			field = "keySource"
		}
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", strings.TrimPrefix(err.Error(), "aisuggest: invalid input: "), map[string]any{"field": field})
		return
	case errors.Is(err, llm.ErrKey) && req.KeySource == aisuggest.SourceServer:
		httpx.WriteError(w, http.StatusBadRequest, "AI_SERVER_KEY_REJECTED", "Anthropic rejected the server's key (LLM_API_KEY); tell the operator, or use your own key", map[string]any{"field": "keySource"})
		return
	case errors.Is(err, llm.ErrKey):
		httpx.WriteError(w, http.StatusBadRequest, "AI_KEY_REJECTED", "Anthropic rejected this API key", map[string]any{"field": "apiKey"})
		return
	case errors.Is(err, llm.ErrModel):
		httpx.WriteError(w, http.StatusBadRequest, "AI_MODEL_UNAVAILABLE", "this key cannot use "+req.Model, map[string]any{"field": "model"})
		return
	case errors.Is(err, llm.ErrRateLimited), errors.Is(err, llm.ErrUnavailable):
		httpx.WriteError(w, http.StatusBadGateway, "AI_UNREACHABLE", "Anthropic could not be reached to check the key; try again", nil)
		return
	case err != nil:
		d.internal(w, r, err)
		return
	}
	if err := d.authz.AuditOrganization(r.Context(), "organization.ai_settings", orgID, actor,
		map[string]any{"model": st.Model, "keySource": req.KeySource, "keyHint": st.KeyHint, "keyChanged": req.APIKey != ""}); err != nil {
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
		"available": access.Available, "source": access.Source, "model": access.Model,
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
	Model         string            `json:"model"`
	KeySource     string            `json:"keySource"`
	Error         string            `json:"error"`
	RequestedBy   string            `json:"requestedBy"`
	CreatedAt     time.Time         `json:"createdAt"`
	FinishedAt    *time.Time        `json:"finishedAt"`
}

func suggestionOut(s aisuggest.Suggestion) suggestionDTO {
	out := suggestionDTO{ID: s.ID, EnvironmentID: s.EnvironmentID, TestType: s.TestType, URL: s.URL, Context: s.Context, Count: s.Count,
		Status: s.Status, Drafts: s.Drafts, PageRead: s.PageRead, Model: s.Model, KeySource: s.KeySource, Error: s.Error,
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
