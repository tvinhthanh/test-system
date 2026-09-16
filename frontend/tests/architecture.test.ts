// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { resolve, relative } from "node:path";
import ts from "typescript";
import { parse } from "@vue/compiler-sfc";
import { it, expect } from "vitest";

const root = resolve("src");
function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((item) =>
    item.isDirectory()
      ? files(resolve(dir, item.name))
      : [resolve(dir, item.name)],
  );
}
it("UI cannot import HTTP/repositories or render raw HTML; core cannot depend on features", () => {
  const errors: string[] = [];
  for (const file of files(root).filter((f) => /\.(vue|ts)$/.test(f))) {
    const name = relative(root, file).replaceAll("\\", "/");
    const original = readFileSync(file, "utf8");
    const component = file.endsWith(".vue");
    const sfc = component ? parse(original).descriptor : null;
    const code = sfc
      ? [sfc.script?.content, sfc.scriptSetup?.content]
          .filter(Boolean)
          .join("\n")
      : original;
    const source = ts.createSourceFile(
      file,
      code,
      ts.ScriptTarget.Latest,
      true,
    );
    if (sfc?.template?.content.match(/\bv-html\b/))
      errors.push(`${name}: raw HTML`);
    function inspect(node: ts.Node) {
      if (
        ts.isImportDeclaration(node) &&
        ts.isStringLiteral(node.moduleSpecifier) &&
        !node.importClause?.isTypeOnly
      ) {
        const dependency = node.moduleSpecifier.text;
        if (component && /(?:core\/api\/client|Repository)$/.test(dependency)) {
          // ApiError is a presentation-safe value; ApiClient is not.
          const names = node.importClause?.namedBindings;
          const errorOnly =
            names &&
            ts.isNamedImports(names) &&
            names.elements.every((e) => e.name.text === "ApiError");
          if (!errorOnly)
            errors.push(`${name}: data-layer import ${dependency}`);
        }
        if (name.startsWith("core/") && /features|\/app\//.test(dependency))
          errors.push(`${name}: dependency inversion`);
      }
      if (
        component &&
        ts.isCallExpression(node) &&
        ["fetch", "axios"].includes(node.expression.getText(source))
      )
        errors.push(`${name}: direct HTTP call`);
      ts.forEachChild(node, inspect);
    }
    inspect(source);
  }
  expect(errors).toEqual([]);
});
