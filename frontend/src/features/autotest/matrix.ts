export const browsers = ["chromium", "firefox", "webkit"] as const;
export const devices = ["desktop", "phone", "tablet"] as const;
export type RunVariant =
  `${(typeof browsers)[number]}/${(typeof devices)[number]}`;
export const defaultVariant: RunVariant = "chromium/desktop";
export const runVariants: RunVariant[] = browsers.flatMap((browser) =>
  devices.map((device) => `${browser}/${device}` as RunVariant),
);

export function variantLabel(value: string): string {
  const [browser, device] = (value || defaultVariant).split("/");
  return `${browser === "webkit" ? "WebKit" : browser === "firefox" ? "Firefox" : "Chromium"} · ${device === "phone" ? "Phone" : device === "tablet" ? "Tablet" : "Desktop"}`;
}

export function matrixSize(caseCount: number, selected: readonly string[]) {
  const unique = new Set(selected);
  const variantCount = unique.size || 1;
  const total = caseCount * variantCount;
  return {
    variantCount,
    total,
    valid:
      caseCount > 0 &&
      selected.length <= 9 &&
      unique.size === selected.length &&
      selected.every((variant) =>
        runVariants.includes(variant as RunVariant),
      ) &&
      total <= 300,
  };
}
