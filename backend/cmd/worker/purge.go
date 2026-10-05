package main

import (
	"context"
	"fmt"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
)

// purger ends deleted projects whose restore window has passed (§11.3.2
// "xóa project"): each module deletes what it owns — artifacts and their
// objects in the bucket first, then triage, flaky, identity, Test
// Management, results, tokens and memberships — and tenancy removes the
// project last, so a purge cut short leaves a deleted project the next
// sweep finishes. The shadow meter's history (billing) stays: it is what
// was counted, not the project's data. Audited as project.purge.
type purger struct {
	tenancy  tenancy.Service
	authz    authz.Service
	artifact artifact.Service
	triage   triage.Service
	flaky    flaky.Service
	identity identity.Service
	testmgmt testmgmt.Service
	results  results.Service
}

// run purges up to limit due projects; it returns how many it finished and
// the first error (a project that failed stays deleted and is retried).
func (p purger) run(ctx context.Context, now time.Time, limit int) (int, error) {
	due, err := p.tenancy.ProjectsToPurge(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	var first error
	for _, d := range due {
		if err := p.one(ctx, d); err != nil {
			if first == nil {
				first = fmt.Errorf("purge %s: %w", d.ID, err)
			}
			continue
		}
		done++
	}
	return done, first
}

func (p purger) one(ctx context.Context, d tenancy.DeletedProject) error {
	id := string(d.ID)
	envs, err := p.tenancy.ListEnvironments(ctx, d.ID)
	if err != nil {
		return err
	}
	envIDs := make([]string, 0, len(envs))
	for _, e := range envs {
		envIDs = append(envIDs, string(e.ID))
	}
	objects, err := p.artifact.PurgeProject(ctx, id)
	if err != nil {
		return err
	}
	for _, step := range []func(context.Context, string) error{
		p.triage.PurgeProject, p.flaky.PurgeProject, p.identity.PurgeProject, p.testmgmt.PurgeProject, p.results.PurgeProject,
	} {
		if err := step(ctx, id); err != nil {
			return err
		}
	}
	if err := p.authz.PurgeProject(ctx, id, envIDs); err != nil {
		return err
	}
	if err := p.tenancy.PurgeProject(ctx, d.ID); err != nil {
		return err
	}
	return p.authz.AuditProject(ctx, "project.purge", id, string(d.WorkspaceID), "system: restore window ended",
		map[string]any{"name": d.Name, "deleted_by": d.DeletedBy, "deleted_at": d.DeletedAt, "artifacts_removed": objects})
}

// runs purges up to limit deleted runs whose restore window has passed
// (§11.3.2 "Xóa dữ liệu run/artifact"): the artifacts (bytes first), the
// triage of their results, review items they raised, then the results and
// the run. Flaky signals were marked for rebuild when the run was deleted.
func (p purger) runs(ctx context.Context, now time.Time, limit int) (int, error) {
	due, err := p.results.RunsToPurge(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	var first error
	for _, d := range due {
		if err := p.run1(ctx, d); err != nil {
			if first == nil {
				first = fmt.Errorf("purge run %s: %w", d.Run.ID, err)
			}
			continue
		}
		done++
	}
	return done, first
}

func (p purger) run1(ctx context.Context, d results.DeletedRun) error {
	list, err := p.results.ListResults(ctx, d.Run.ID)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(list))
	for _, r := range list {
		ids = append(ids, string(r.ID))
	}
	objects, err := p.artifact.PurgeRun(ctx, string(d.Run.ID))
	if err != nil {
		return err
	}
	if err := p.triage.PurgeResults(ctx, ids); err != nil {
		return err
	}
	if err := p.identity.PurgeRun(ctx, string(d.Run.ID)); err != nil {
		return err
	}
	if err := p.results.PurgeRun(ctx, d.Run.ID); err != nil {
		return err
	}
	return p.authz.AuditRun(ctx, "run.purge", string(d.Run.ID), d.Run.ProjectID, "system: restore window ended",
		map[string]any{"deleted_by": d.DeletedBy, "deleted_at": d.DeletedAt, "results_removed": len(ids), "artifacts_removed": objects})
}

// orgs purges up to limit deleted organizations whose restore window has
// passed (§11.3.2 "Billing, xóa org"): every project under it the way one
// deleted project is purged (one() audits each as project.purge), then the
// memberships, invitations and tokens held at the org and its workspaces,
// then the workspaces and the org. A purge cut short leaves a deleted org
// with fewer projects, which the next sweep finishes. The shadow meter's
// history stays, as for a project.
func (p purger) orgs(ctx context.Context, now time.Time, limit int) (int, error) {
	due, err := p.tenancy.OrganizationsToPurge(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	done := 0
	var first error
	for _, d := range due {
		if err := p.org1(ctx, d); err != nil {
			if first == nil {
				first = fmt.Errorf("purge organization %s: %w", d.ID, err)
			}
			continue
		}
		done++
	}
	return done, first
}

func (p purger) org1(ctx context.Context, d tenancy.DeletedOrganization) error {
	projects, err := p.tenancy.OrganizationProjects(ctx, d.ID)
	if err != nil {
		return err
	}
	for _, project := range projects {
		if err := p.one(ctx, project); err != nil {
			return err
		}
	}
	ws := make([]string, 0, len(d.WorkspaceIDs))
	for _, w := range d.WorkspaceIDs {
		ws = append(ws, string(w))
	}
	if err := p.authz.PurgeOrganization(ctx, string(d.ID), ws); err != nil {
		return err
	}
	if err := p.tenancy.PurgeOrganization(ctx, d.ID); err != nil {
		return err
	}
	return p.authz.AuditOrganization(ctx, "organization.purge", string(d.ID), "system: restore window ended",
		map[string]any{"name": d.Name, "slug": d.Slug, "deleted_by": d.DeletedBy, "deleted_at": d.DeletedAt, "projects_removed": len(projects), "workspaces_removed": len(ws)})
}
