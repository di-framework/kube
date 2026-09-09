import { expect, test } from "bun:test";
import { featureGates, requireGates, type VerificationGate } from "../shared/verification-gates";

test("missing deployment or live probes cannot produce a successful report", () => {
  const required = featureGates(["static-site", "durable-receipts"]);
  const steps: VerificationGate[] = required.filter((name) => !name.endsWith("-live") && !name.endsWith("-deploy")).map((name) => ({ name, passed: true }));
  expect(requireGates(steps, required)).toBe(false);
  expect(steps.filter((step) => !step.passed).map((step) => step.name)).toEqual([
    "static-site-deploy", "static-site-live", "durable-receipts-deploy", "durable-receipts-live",
  ]);
});

test("an explicit failure or an empty gate set cannot pass", () => {
  expect(requireGates([], [])).toBe(false);
  expect(requireGates([{ name: "build", passed: false }], ["build"])).toBe(false);
  expect(requireGates([{ name: "build", passed: true }], ["build"])).toBe(true);
});
