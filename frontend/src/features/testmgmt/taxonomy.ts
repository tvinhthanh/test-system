export const levels = ["unit", "integration", "system", "acceptance"] as const;
export const purposes = [
  "functional",
  "performance",
  "security",
  "usability",
  "accessibility",
  "compatibility",
  "recovery",
] as const;
export const usageTags = ["smoke", "regression", "e2e"] as const;
export const executionModes = ["manual", "automated"] as const;
export const performanceTypes = [
  "load",
  "stress",
  "spike",
  "soak",
  "volume",
  "scalability",
] as const;
export interface Taxonomy {
  level: "" | (typeof levels)[number];
  purposes: (typeof purposes)[number][];
  tags: (typeof usageTags)[number][];
  executionModes: (typeof executionModes)[number][];
  performanceTypes: (typeof performanceTypes)[number][];
}
export function copyTaxonomy(value?: Taxonomy): Taxonomy {
  return {
    level: value?.level ?? "",
    purposes: [...(value?.purposes ?? [])],
    tags: [...(value?.tags ?? [])],
    executionModes: [...(value?.executionModes ?? [])],
    performanceTypes: [...(value?.performanceTypes ?? [])],
  };
}
export const taxonomyLabel = (value: string) =>
  value === "e2e"
    ? "E2E"
    : value === "soak"
      ? "Soak / Endurance"
      : value.charAt(0).toUpperCase() + value.slice(1);
export function taxonomySummary(value?: Taxonomy | null) {
  if (!value) return "Classification unavailable";
  return (
    [
      value.level,
      ...value.purposes,
      ...value.tags,
      ...value.executionModes,
      ...value.performanceTypes,
    ]
      .filter(Boolean)
      .map(taxonomyLabel)
      .join(" / ") || "Unclassified"
  );
}
export const taxonomyFilterOptions = {
  level: { label: "Level", values: levels },
  purpose: { label: "Purpose", values: purposes },
  tag: { label: "Usage", values: usageTags },
  executionMode: { label: "Execution mode", values: executionModes },
  performanceType: { label: "Performance type", values: performanceTypes },
};
export type TaxonomyFilter = Partial<
  Record<keyof typeof taxonomyFilterOptions, string>
>;
