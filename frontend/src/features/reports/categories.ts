// §8.7 failureCategory — a closed enum shared by people and the AI. Fixed
// order: it is also the series order of every chart, never re-assigned when
// a category is absent in a window. `unknown` means "not triaged yet".
export const failureCategories = [
  "app_bug",
  "test_bug",
  "flaky_timing",
  "env_infra",
  "env_data",
  "dependency_external",
  "unknown",
] as const;
export type FailureCategory = (typeof failureCategories)[number];

export const categoryLabel: Record<FailureCategory, string> = {
  app_bug: "App bug",
  test_bug: "Test bug",
  flaky_timing: "Flaky / timing",
  env_infra: "Environment / infra",
  env_data: "Test data",
  dependency_external: "External dependency",
  unknown: "Not triaged",
};

export function isFailureCategory(value: string): value is FailureCategory {
  return (failureCategories as readonly string[]).includes(value);
}
