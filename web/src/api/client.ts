import { apiBase } from "../config";

/** A failed call, carrying what the API said rather than a generic message. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

/** The problem details shape every service answers errors with. */
type Problem = {
  title?: string;
  detail?: string;
  validationErrors?: Record<string, string>;
};

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });

  if (!response.ok) {
    throw new ApiError(response.status, await describe(response));
  }

  // 204 and friends have nothing to decode.
  if (response.status === 204 || response.headers.get("Content-Length") === "0") {
    return undefined as T;
  }

  return (await response.json()) as T;
}

/** describe turns a failed response into something worth showing a person. */
async function describe(response: Response): Promise<string> {
  try {
    const problem = (await response.json()) as Problem;
    const fields = problem.validationErrors
      ? Object.values(problem.validationErrors).join(", ")
      : "";

    return fields || problem.detail || problem.title || response.statusText;
  } catch {
    // An error that is not problem details - a proxy timing out, say.
    return response.statusText || `request failed with ${response.status}`;
  }
}

export const api = {
  get: <T>(path: string) => request<T>(path),
  post: <T>(path: string, body: unknown) =>
    request<T>(path, { method: "POST", body: JSON.stringify(body) }),
  delete: <T>(path: string) => request<T>(path, { method: "DELETE" }),
};
