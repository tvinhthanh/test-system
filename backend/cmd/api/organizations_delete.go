package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Deleting an organization (§11.3.2 "Billing, xóa org" — owners only) is
// soft, like a project: from the answer on, tenancy no longer returns the
// org, its workspaces or its projects, so every route and every CI token
// under it is a 404. Its owners see it under "deleted organizations" and
// can restore it for orgRestoreWindow; then cmd/worker purges every
// project, the org's memberships and tokens, the workspaces and the org.
// The body carries the org's exact name. Owners are MFA-enrolled already
// (§11.2), so the session doing this passed a second factor. Delete,
// restore and purge are audited.
//
//	DELETE /v1/organizations/{organizationId}           {confirmName}
//	GET    /v1/organizations/deleted
//	POST   /v1/organizations/{organizationId}/restore

const orgRestoreWindow = 30 * 24 * time.Hour

func (d *dashboard) orgDeleteRoutes(handle func(pattern string, fn http.HandlerFunc)) {
	handle("DELETE /v1/organizations/{organizationId}", d.deleteOrganization)
	handle("GET /v1/organizations/deleted", d.deletedOrganizations)
	handle("POST /v1/organizations/{organizationId}/restore", d.restoreOrganization)
}

type deletedOrgDTO struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	DeletedAt  time.Time `json:"deletedAt"`
	DeletedBy  string    `json:"deletedBy"`
	PurgeAfter time.Time `json:"purgeAfter"`
}

func deletedOrgOut(o tenancy.DeletedOrganization) deletedOrgDTO {
	return deletedOrgDTO{ID: string(o.ID), Name: o.Name, Slug: o.Slug, DeletedAt: o.DeletedAt, DeletedBy: o.DeletedBy, PurgeAfter: o.PurgeAfter}
}

func (d *dashboard) deleteOrganization(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	org, err := d.tenancy.GetOrganization(r.Context(), tenancy.OrganizationID(r.PathValue("organizationId")))
	if err != nil {
		d.nodeErr(w, r, err, "ORGANIZATION_NOT_FOUND", "organization not found")
		return
	}
	if !d.authorizeNode(w, r, p, authz.Resource{OrgID: string(org.ID)}, authz.ActionOrgManage, "ORGANIZATION_NOT_FOUND", "organization not found") {
		return
	}
	var req struct {
		ConfirmName string `json:"confirmName"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.ConfirmName) != org.Name {
		httpx.WriteError(w, http.StatusBadRequest, "CONFIRM_NAME", "type the organization's name to delete it", map[string]any{"field": "confirmName"})
		return
	}
	actor := d.actor(r, p)
	gone, err := d.tenancy.DeleteOrganization(r.Context(), org.ID, actor, orgRestoreWindow)
	if err != nil {
		d.nodeErr(w, r, err, "ORGANIZATION_NOT_FOUND", "organization not found")
		return
	}
	if err := d.authz.AuditOrganization(r.Context(), "organization.delete", string(gone.ID), actor,
		map[string]any{"name": gone.Name, "slug": gone.Slug, "purge_after": gone.PurgeAfter}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deletedOrgOut(gone))
}

// ownedDeleted is the deleted organizations the caller may restore: those
// where a membership of theirs gives org.manage.
func (d *dashboard) ownedDeleted(r *http.Request, p authz.Principal) ([]tenancy.DeletedOrganization, error) {
	ms, err := d.authz.ListMemberships(r.Context(), p.UserID)
	if err != nil {
		return nil, err
	}
	var ids []tenancy.OrganizationID
	for _, m := range ms {
		if m.Scope.Level == authz.LevelOrganization {
			ids = append(ids, tenancy.OrganizationID(m.Scope.ID))
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	list, err := d.tenancy.DeletedOrganizations(r.Context(), ids)
	if err != nil {
		return nil, err
	}
	out := []tenancy.DeletedOrganization{}
	for _, o := range list {
		switch err := d.authz.Authorize(r.Context(), p, authz.Resource{OrgID: string(o.ID)}, authz.ActionOrgManage); {
		case err == nil:
			out = append(out, o)
		case errors.Is(err, authz.ErrForbidden), errors.Is(err, authz.ErrNoAccess):
		default:
			return nil, err
		}
	}
	return out, nil
}

func (d *dashboard) deletedOrganizations(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	list, err := d.ownedDeleted(r, p)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listOut(list, deletedOrgOut))
}

func (d *dashboard) restoreOrganization(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	list, err := d.ownedDeleted(r, p)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	id := r.PathValue("organizationId")
	found := false
	for _, o := range list {
		found = found || string(o.ID) == id
	}
	if !found { // live, purged, not theirs, or theirs without org.manage
		httpx.WriteError(w, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "no deleted organization with that id that you own", nil)
		return
	}
	org, err := d.tenancy.RestoreOrganization(r.Context(), tenancy.OrganizationID(id))
	if err != nil {
		d.nodeErr(w, r, err, "ORGANIZATION_NOT_FOUND", "no deleted organization with that id that you own")
		return
	}
	if err := d.authz.AuditOrganization(r.Context(), "organization.restore", id, d.actor(r, p), map[string]any{"name": org.Name}); err != nil {
		d.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": string(org.ID), "name": org.Name, "slug": org.Slug})
}
