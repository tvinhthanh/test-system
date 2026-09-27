import { shallowRef } from "vue";
import type { MemberScope, MembersRepository } from "./membersRepository";
import type { Level } from "./roles";

// The organizations and workspaces the signed-in user manages, from the
// server (GET /v1/member-scopes): whether to show "Team", and what a team
// screen may offer. Like ProjectAccess, it belongs to one user and is
// cleared when that user changes; the server still checks every call.
export class TeamAccess {
  private state = shallowRef<MemberScope[] | null>(null);
  private loading: Promise<void> | null = null;

  constructor(private members: MembersRepository) {}

  get scopes(): MemberScope[] {
    return this.state.value ?? [];
  }

  ensure() {
    if (this.state.value) return Promise.resolve();
    this.loading ??= this.members
      .scopes(new AbortController().signal)
      .then((scopes) => {
        this.state.value = scopes;
      })
      // Not cached: an owner held at MFA enrolment (403) gets the answer
      // on a later try, once enrolled. Until then there is nothing to show.
      .catch(() => undefined)
      .finally(() => {
        this.loading = null;
      });
    return this.loading;
  }

  find(level: Level, id: string) {
    return this.scopes.find((s) => s.level === level && s.id === id);
  }

  clear() {
    this.state.value = null;
    this.loading = null;
  }
}
