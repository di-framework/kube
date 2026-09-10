export interface VerificationGate {
  name: string;
  passed: boolean;
  detail?: string;
  log?: string;
}

/** Missing evidence is a failed gate, including probes skipped after failed deployment. */
export function requireGates(steps: VerificationGate[], names: readonly string[]): boolean {
  for (const name of names) {
    if (!steps.some((step) => step.name === name)) {
      steps.push({ name, passed: false, detail: "Required gate did not run" });
    }
  }
  return names.length > 0 && steps.every((step) => step.passed);
}

export function featureGates(apps: readonly string[]): string[] {
  return [
    "prepare", "typecheck", "local-tests", "runtime-version", "greeter-build", "candidate-smoke",
    ...apps.flatMap((name) => ["build", "artifact", "manifest", "server-validation", "deploy", "live"].map((gate) => `${name}-${gate}`)),
  ];
}
