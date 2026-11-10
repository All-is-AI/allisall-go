// Tests of the generator. Run with `node --test scripts/`, from the package
// directory. They need the API's OpenAPI document, which exists only in the
// monorepo, and Go, to run oapi-codegen; without the document they skip.

import assert from "node:assert/strict";
import {
  existsSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";
import { parse, stringify } from "yaml";
import {
  checkOverlay,
  generate,
  OUTPUTS,
  preload,
  SCALARS,
  SPEC_PATH,
  sdkOperations,
  staleFiles,
} from "./generate.mjs";

const packageDir = join(dirname(fileURLToPath(import.meta.url)), "..");
const skip = existsSync(SPEC_PATH)
  ? false
  : "the API's document is absent outside the monorepo";
const specText = skip ? "" : readFileSync(SPEC_PATH, "utf8");
const overlay = parse(readFileSync(join(packageDir, "overlay.yaml"), "utf8"));

/** A fresh parse of the document, for a test to change. */
const freshSpec = () => parse(specText);

/** The committed generated files. */
function committed() {
  return Object.fromEntries(
    Object.values(OUTPUTS).map((f) => [
      f,
      readFileSync(join(packageDir, f), "utf8"),
    ]),
  );
}

describe("the committed files", { skip }, () => {
  const generated = skip ? {} : generate(specText);

  it("equal what the document generates", () => {
    assert.deepEqual(staleFiles(generated, packageDir), []);
  });

  it("are stale when one generated line is edited by hand", () => {
    const dir = mkdtempSync(join(tmpdir(), "allisall-go-check-"));
    try {
      for (const [file, content] of Object.entries(committed())) {
        writeFileSync(join(dir, file), content);
      }
      assert.deepEqual(staleFiles(generated, dir), []);
      const types = readFileSync(join(dir, OUTPUTS.types), "utf8");
      const edited = types.replace(
        '\tAgentStatusActive  AgentStatus = "active"',
        '\tAgentStatusActive  AgentStatus = "enabled"',
      );
      assert.notEqual(edited, types, "the fixture line is not in the file");
      writeFileSync(join(dir, OUTPUTS.types), edited);
      assert.deepEqual(staleFiles(generated, dir), [OUTPUTS.types]);
    } finally {
      rmSync(dir, { recursive: true, force: true });
    }
  });

  it("are stale, types alone, when one description changes", () => {
    const spec = freshSpec();
    spec.components.schemas.ClientCreate.description += " Changed.";
    const files = generate(stringify(spec));
    assert.deepEqual(staleFiles(files, packageDir), [OUTPUTS.types]);
  });
});

describe("the pre-load step", { skip }, () => {
  const spec = skip ? {} : freshSpec();
  const operations = skip ? [] : sdkOperations(spec);
  const out = skip ? {} : preload(spec, operations);

  it("keeps the 58 operations an API key or no credential can call", () => {
    assert.equal(operations.length, 58);
    for (const op of operations) {
      assert.ok(
        ["KeyOnly", "KeyOrSession", "Public"].includes(op.access),
        op.id,
      );
    }
    assert.ok(!operations.some((op) => op.id === "getProblemType"));
    for (const id of ["redeemInvite", "createWorkspace", "getInvite"]) {
      assert.ok(!operations.some((op) => op.id === id), id);
    }
    for (const id of [
      "getWorkspace",
      "getOnboarding",
      "setOnboardingSkippedSteps",
    ]) {
      assert.ok(
        operations.some((op) => op.id === id),
        id,
      );
    }
  });

  it("generates 114 schema types, ClientRecord among them and no scalar", () => {
    const config = parse(
      readFileSync(join(packageDir, "oapi-codegen.yaml"), "utf8"),
    );
    const excluded = config["output-options"]["exclude-schemas"];
    const names = Object.keys(out.components.schemas).filter(
      (n) => !excluded.includes(n),
    );
    assert.equal(names.length, 114);
    assert.ok(names.includes("ClientRecord"));
    for (const name of [
      "Workspace",
      "Onboarding",
      "OnboardingStepProgress",
      "OnboardingStep",
      "OnboardingSkippedStepsSet",
    ]) {
      assert.ok(names.includes(name), name);
    }
    // Reached only by the operations the SDK leaves out.
    for (const name of ["InviteRedemption", "WorkspaceCreate"]) {
      assert.ok(!names.includes(name), name);
    }
    assert.ok(!names.includes("Client"));
    for (const scalar of SCALARS) assert.ok(!names.includes(scalar), scalar);
    const types = committed()[OUTPUTS.types];
    for (const name of names) {
      assert.match(
        types,
        new RegExp(`^type ${name} `, "m"),
        `types_gen.go declares no ${name}`,
      );
    }
  });

  it("makes every Money value a Decimal and leaves no reference to it", () => {
    const text = JSON.stringify(out);
    assert.ok(!text.includes("#/components/schemas/Money"));
    const money = JSON.stringify(spec).match(
      /"#\/components\/schemas\/Money"/g,
    ).length;
    const decimals = text.match(/"x-go-type":"Decimal"/g).length;
    // Each reference to Money becomes one Decimal, but only in the schemas
    // the operations reach.
    assert.ok(
      decimals > 0 && decimals <= money,
      `${decimals} Decimals for ${money} references`,
    );
  });

  it("collapses each open set to its closed set", () => {
    assert.deepEqual(out.components.schemas.ClientRecord.properties.status, {
      $ref: "#/components/schemas/ClientStatus",
    });
    assert.deepEqual(out.components.schemas.Health.properties.status.enum, [
      "ok",
    ]);
    assert.equal(
      out.components.schemas.Health.properties.status.anyOf,
      undefined,
    );
  });

  it("makes an integer of one value a plain integer", () => {
    const version =
      out.components.schemas.CalculationSnapshot.allOf[0].properties
        .snapshot_version;
    assert.deepEqual(version, { type: "integer" });
    // The spec writes it as a const; a one-value enum is dropped the same
    // way, and an integer enum of two values is kept.
    const changed = freshSpec();
    const props = changed.components.schemas.ClientEventList.properties;
    props.total = { type: "integer", enum: [1] };
    props.page = { type: "integer", enum: [1, 2] };
    const rewritten = preload(changed, sdkOperations(changed));
    const { total, page } =
      rewritten.components.schemas.ClientEventList.properties;
    assert.deepEqual(total, { type: "integer" });
    assert.deepEqual(page.enum, [1, 2]);
  });

  it("generates no type for an integer of one value", () => {
    const types = committed()[OUTPUTS.types];
    assert.doesNotMatch(types, /SnapshotVersion(N1| int$)/m);
    assert.match(types, /SnapshotVersion int +`json:"snapshot_version"`/);
  });

  it("keeps each operation's query alone", () => {
    const op = out.paths["/v1/clients/{id}/events"].get;
    assert.deepEqual(Object.keys(op).sort(), [
      "operationId",
      "parameters",
      "responses",
    ]);
    assert.ok(op.parameters.every((p) => p.in === "query" || p.in === "path"));
    assert.equal(out.paths["/v1/clients"].post, undefined);
    assert.equal(out.components.requestBodies, undefined);
    assert.equal(out.components.responses, undefined);
  });

  it("removes the tab from the delimiter, and leaves the document it read unchanged", () => {
    assert.deepEqual(
      out.components.schemas.ImportMapping.properties.delimiter.type,
      "string",
    );
    assert.equal(
      out.components.schemas.ImportMapping.properties.delimiter.enum,
      undefined,
    );
    assert.deepEqual(
      spec.components.schemas.ImportMapping.properties.delimiter.enum,
      [",", ";", "\t"],
    );
  });

  it("makes the response's open ImportDelimiter a string, after collapsing its anyOf", () => {
    const { ImportDelimiter, AppliedImportMapping } = out.components.schemas;
    assert.equal(ImportDelimiter.type, "string");
    assert.equal(ImportDelimiter.enum, undefined);
    assert.equal(ImportDelimiter["x-enum-varnames"], undefined);
    // The open set's anyOf is gone, and the member names the component.
    const member = AppliedImportMapping.properties.delimiter;
    assert.equal(member.$ref, "#/components/schemas/ImportDelimiter");
    assert.equal(member.anyOf, undefined);
    assert.deepEqual(spec.components.schemas.ImportDelimiter.enum, [
      ",",
      ";",
      "\t",
    ]);
  });
});

describe("the script refuses", { skip }, () => {
  it("a nullable patch member the Overlay does not name, and names it", () => {
    const spec = freshSpec();
    spec.components.schemas.ValueModelPatch.properties.extra = {
      anyOf: [{ type: "string" }, { type: "null" }],
    };
    assert.throws(
      () => checkOverlay(spec, sdkOperations(spec), overlay),
      /\$\.components\.schemas\.ValueModelPatch\.properties\.extra .*does not name it/,
    );
  });

  it("an Overlay member whose type is not its value's", () => {
    const spec = freshSpec();
    spec.components.schemas.ReportPatch.properties.note = {
      anyOf: [{ type: "integer" }, { type: "null" }],
    };
    assert.throws(
      () => checkOverlay(spec, sdkOperations(spec), overlay),
      /ReportPatch\.properties\.note the type Nullable\[string\], but its value is int/,
    );
  });

  it("an operation with two 2xx responses, or no x-access, or no boolean x-idempotent", () => {
    const two = freshSpec();
    two.paths["/v1/clients"].get.responses["201"] = { description: "x" };
    assert.throws(
      () => sdkOperations(two),
      /listClients has more than one 2xx response/,
    );
    const access = freshSpec();
    delete access.paths["/v1/clients"].get["x-access"];
    assert.throws(
      () => sdkOperations(access),
      /listClients has no x-access policy/,
    );
    const idem = freshSpec();
    idem.paths["/v1/clients"].get["x-idempotent"] = "no";
    assert.throws(
      () => sdkOperations(idem),
      /listClients has no boolean x-idempotent/,
    );
  });

  it("an operation under a policy it neither calls nor leaves out, and names both", () => {
    // Skipping the operation would fail this as surely as taking it in.
    const spec = freshSpec();
    spec.paths["/v1/widgets"] = {
      get: {
        ...structuredClone(spec.paths["/v1/agents"].get),
        operationId: "listWidgets",
        "x-access": { policy: "PartnerToken" },
      },
    };
    assert.throws(
      () => sdkOperations(spec),
      /listWidgets has the x-access policy PartnerToken/,
    );
  });

  it("a request body the SDK does not send", () => {
    const spec = freshSpec();
    const op = spec.paths["/v1/clients"].post;
    op.requestBody = {
      content: { "text/plain": { schema: { type: "string" } } },
    };
    assert.throws(
      () => sdkOperations(spec),
      /createClient takes a request body the SDK does not send/,
    );
  });

  it("a $ref that is not local or does not resolve", () => {
    const remote = freshSpec();
    remote.components.schemas.ClientCreate.properties.name = {
      $ref: "other.yaml#/X",
    };
    assert.throws(
      () => generate(stringify(remote)),
      /is not a local reference/,
    );
    const dangling = freshSpec();
    dangling.components.schemas.ClientCreate.properties.name = {
      $ref: "#/components/schemas/Nope",
    };
    assert.throws(() => generate(stringify(dangling)), /does not resolve/);
  });

  it("a delimiter added or removed, but not a reorder", () => {
    const withDelimiters = (list) => {
      const spec = freshSpec();
      spec.components.schemas.ImportMapping.properties.delimiter.enum = list;
      return () => preload(spec, sdkOperations(spec));
    };
    for (const list of [
      [",", ";", "\t", "|"],
      [",", ";"],
      [",", ";", ";"],
      [",", ";", ";", "\t"],
    ]) {
      assert.throws(
        withDelimiters(list),
        /ImportMapping\.delimiter lists/,
        JSON.stringify(list),
      );
    }
    for (const list of [
      ["\t", ";", ","],
      [";", ",", "\t"],
    ]) {
      const out = withDelimiters(list)();
      assert.equal(
        out.components.schemas.ImportMapping.properties.delimiter.type,
        "string",
      );
    }
  });

  it("an ImportDelimiter set added to or removed from, but not a reorder", () => {
    const withDelimiters = (list) => {
      const spec = freshSpec();
      spec.components.schemas.ImportDelimiter.enum = list;
      return () => preload(spec, sdkOperations(spec));
    };
    for (const list of [
      [",", ";", "\t", "|"],
      [",", ";"],
      [",", ";", ";", "\t"],
    ]) {
      assert.throws(
        withDelimiters(list),
        /ImportDelimiter lists/,
        JSON.stringify(list),
      );
    }
    const out = withDelimiters(["\t", ",", ";"])();
    assert.equal(out.components.schemas.ImportDelimiter.type, "string");
  });

  it("an Overlay entry for an enum member that is not a Nullable of the enum's type", () => {
    // The committed Overlay declares ClientPatch.renewal_term as
    // Nullable[RenewalTerm], the enum's own type.
    const spec = freshSpec();
    assert.doesNotThrow(() => checkOverlay(spec, sdkOperations(spec), overlay));
    const changed = structuredClone(overlay);
    const entry = changed.actions.find(
      (a) =>
        a.target === "$.components.schemas.ClientPatch.properties.renewal_term",
    );
    entry.update["x-go-type"] = "Nullable[string]";
    assert.throws(
      () => checkOverlay(spec, sdkOperations(spec), changed),
      /ClientPatch\.properties\.renewal_term the type Nullable\[string\], but its value is RenewalTerm/,
    );
  });

  it("an Overlay entry tagged other than exactly omitzero", () => {
    const spec = freshSpec();
    const changed = structuredClone(overlay);
    delete changed.actions[0].update["x-omitempty"];
    assert.throws(
      () => checkOverlay(spec, sdkOperations(spec), changed),
      /AgentPatch\.properties\.framework .*not/,
    );
  });
});
