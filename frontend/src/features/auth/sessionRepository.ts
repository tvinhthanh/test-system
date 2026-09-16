import type { ApiClient } from "../../core/api/client";
import { record, string } from "../../core/api/decode";

export interface User {
  id: string;
  name: string;
  email: string;
}
export interface Session {
  user: User | null;
  csrfToken: string;
}
function parseSession(value: unknown): Session {
  const json = record(value);
  const user = json.user === null ? null : record(json.user);
  return {
    csrfToken: string(json.csrfToken),
    user: user
      ? {
          id: string(user.id),
          name: string(user.name),
          email: string(user.email),
        }
      : null,
  };
}

export class SessionRepository {
  constructor(private api: ApiClient) {}
  async bootstrap() {
    return parseSession(await this.api.request("/v1/session"));
  }
  async login(email: string, password: string) {
    return parseSession(
      await this.api.request("/v1/session", { body: { email, password } }),
    );
  }
  async logout() {
    await this.api.request("/v1/session/logout", { body: {} });
  }
}
