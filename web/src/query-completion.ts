export type CompletionMode = "symbol" | "selector" | "relation" | "filter" | "value";
export type QueryCompletionContext = {
  mode: CompletionMode;
  prefix: string;
  start: number;
  end: number;
  filter?: "-f" | "+pkg" | "-pkg";
};
export type QueryCompletion = { label: string; insert: string; help: string };

const relations: QueryCompletion[] = [
  { label: "<", insert: "< ", help: "Incoming callers or references" },
  { label: ">", insert: "> ", help: "Outgoing calls" },
  { label: "=", insert: "= ", help: "Definition" },
  { label: "<<3", insert: "<<3 ", help: "Callers up to three hops away; depth 1–8" },
  { label: ":impl", insert: ":impl ", help: "Types implementing an interface" },
  { label: ":methods", insert: ":methods ", help: "Methods owned by a type" },
  { label: "~w", insert: "~w ", help: "Write references" },
  { label: "&", insert: "& ", help: "Intersect the result symbols with another expression" },
  { label: "|", insert: "| ", help: "Union the result symbols with another expression" },
  { label: ">>", insert: ">> ", help: "Shortest call path to another symbol; depth 1–8" },
];

const filters: QueryCompletion[] = [
  { label: "-f", insert: "-f ", help: "Exclude a file suffix, such as _test.go" },
  { label: "+pkg", insert: "+pkg ", help: "Keep a package; append /... for its subpackages" },
  { label: "-pkg", insert: "-pkg ", help: "Exclude a package; append /... for its subpackages" },
];

const selectors: QueryCompletion[] = [
  { label: "pkg:", insert: "pkg:", help: "Package path or module pattern and relative package pattern" },
  { label: "mod:", insert: "mod:", help: "Registered module root" },
  { label: "func:", insert: "func:", help: "Function or method" },
  { label: "field:", insert: "field:", help: "Struct field" },
  { label: "struct:", insert: "struct:", help: "Defined struct type" },
];
const typedModifiers: QueryCompletion[] = selectors.flatMap((option) => [
  { ...option, label: `+${option.label}`, insert: `+${option.insert}`, help: `Include ${option.help.toLowerCase()}` },
  { ...option, label: `-${option.label}`, insert: `-${option.insert}`, help: `Exclude ${option.help.toLowerCase()}` },
]);

const tokenPattern = /[+-]?(?:pkg|mod|func|field|struct):[A-Za-z0-9_./*?@#$!\-]*(?::[A-Za-z0-9_./*?@#$!\-]*)?|<<\d*|>>\d*|:[A-Za-z]*|~[A-Za-z]*|[+-][A-Za-z]*|[<>=&|()]|[A-Za-z_][A-Za-z0-9_./*-]*/g;
type Token = { text: string; start: number; end: number };

export function queryCompletionContext(draft: string, cursor: number): QueryCompletionContext {
  const tokens: Token[] = Array.from(draft.matchAll(tokenPattern), (match) => ({
    text: match[0], start: match.index, end: match.index + match[0].length,
  }));
  const current = tokens.find((token) => token.start < cursor && cursor <= token.end);
  let mode: CompletionMode = "symbol";
  let filter: QueryCompletionContext["filter"];
  for (const token of tokens) {
    if (token.end > (current?.start ?? cursor)) break;
    if (token.text === "(" && mode === "symbol") continue;
    if (token.text === ")") { mode = "relation"; continue; }
    if (token.text === "&" || token.text === "|" || /^>>\d*$/.test(token.text)) { mode = "symbol"; continue; }
    if (token.text === "-f" || token.text === "+pkg" || token.text === "-pkg") {
      mode = "value";
      filter = token.text;
      continue;
    }
	if (/^[+-]?(?:pkg|mod|func|field|struct):/.test(token.text)) { mode = "relation"; continue; }
    if (mode === "value") { mode = "filter"; filter = undefined; continue; }
    if (/^(?:<<\d*|<|>|=|:impl|:methods|~w)$/.test(token.text)) { mode = "filter"; continue; }
    if (mode === "symbol") mode = "relation";
  }
  if (current && cursor === current.end && (current.text === "(" || current.text === ")" ||
    current.text === "&" || current.text === "|" || /^>>\d*$/.test(current.text))) {
    return { mode: current.text === ")" ? "relation" : "symbol", prefix: "", start: cursor, end: cursor };
  }
  const prefix = current ? draft.slice(current.start, cursor) : "";
  if (/^[+-]?(?:pkg|mod|func|field|struct):/.test(current?.text ?? "")) mode = "selector";
  else if (mode === "filter" && /^[A-Za-z_]/.test(current?.text ?? "")) mode = "symbol";
  return {
    mode, prefix,
    start: current?.start ?? cursor, end: current?.end ?? cursor,
    ...(mode === "value" ? { filter } : {}),
  };
}

export function querySyntaxCompletions(context: QueryCompletionContext, moduleRoot = ""): QueryCompletion[] {
  if (context.mode === "symbol") return selectors.filter((option) => option.label.startsWith(context.prefix));
  if (context.mode === "selector") return [];
  if (context.mode === "value") {
    const values: QueryCompletion[] = context.filter === "-f"
      ? [{ label: "_test.go", insert: "_test.go ", help: "Exclude Go test files" }]
      : moduleRoot ? [
        { label: moduleRoot, insert: `${moduleRoot} `, help: "This module's root package" },
        { label: `${moduleRoot}/...`, insert: `${moduleRoot}/... `, help: "This module and every subpackage" },
      ] : [];
    return values.filter((option) => option.label.startsWith(context.prefix));
  }
  const options = context.mode === "relation" ? [...typedModifiers, ...relations] : [...filters, ...selectors, ...relations];
  return options.filter((option) => option.label.startsWith(context.prefix));
}
