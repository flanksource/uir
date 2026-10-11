import type { QueryCatalog, QueryLanguage } from "./api";

export function isSQL(expression: string, language: QueryLanguage = "auto"): boolean {
  return language === "sql" || (language === "auto" && /^\s*SELECT(?=$|[^\p{L}\p{N}_])/iu.test(expression));
}

export function sqlCompletions(expression: string, offset: number, catalog: QueryCatalog): { start: number; end: number; choices: { label: string; insert: string; detail: string }[]; selector?: string } {
  const before = expression.slice(0, offset);
  const via = /\bvia\s*=>\s*'([^']*)$/i.exec(before);
  const prefix = via ? via[1].split(",").at(-1)!.trimStart() : /[\p{L}\p{N}_./:*?"\\-]*$/u.exec(before)![0];
  const start = offset - prefix.length;
  const choices: { label: string; insert: string; detail: string }[] = [];
  const add = (label: string, detail: string, insert = label) => {
    if (label.toLowerCase().startsWith(prefix.toLowerCase())) choices.push({ label, insert, detail });
  };
  if (via) {
    Object.keys(catalog.domains.relationships).sort().forEach((name) => add(name, "Native relationship"));
    return { start, end: offset, choices };
  }
  if (/^[\p{L}_][\p{L}\p{N}_.]*:/u.test(prefix)) return { start, end: offset, choices, selector: prefix };
  const source = /\bIN\s+[^\s,()]*$/i.test(before) || (/\bFROM\s+[\s\S]*$/i.test(before) && !/\b(WHERE|ORDER\s+BY|LIMIT)\b/i.test(before));
  if (source) {
    [...new Set(["pkg", "mod", "kind", "api", ...catalog.kinds.map((kind) => kind.name)])].forEach((name) => add(`${name}:`, "Symbol selector", `${name}:`));
    catalog.functions.forEach((fn) => add(fn.name, fn.terminal ? "Graph or path result" : "Navigation", `${fn.name}(`));
    const functions = [...before.matchAll(/([\p{L}_][\p{L}\p{N}_]*)\(/gu)];
    const fn = catalog.functions.find((fn) => fn.name === functions.at(-1)?.[1].toLowerCase());
    fn?.options?.forEach((option) => add(option, "Named option", `${option} => `));
  } else {
    const alias = /\bFROM\s+[\s\S]*?\bAS\s+([\p{L}_][\p{L}\p{N}_]*)\s*(?:WHERE|ORDER|LIMIT|$)/iu.exec(expression)?.[1];
    Object.entries(catalog.fields).forEach(([field, type]) => {
      add(field, type);
      if (alias && field.startsWith("symbol.props.")) add(`${alias}.${field.slice("symbol.props.".length)}`, type);
    });
  }
  ["SELECT", "FROM", "AS", "WHERE", "ORDER BY", "LIMIT", "MATCH", "IMATCH", "IN", "NOT", "AND", "OR", "IS NULL", "IS NOT NULL", "ASC", "DESC"].forEach((word) => add(word, "SQL keyword", `${word} `));
  return { start, end: offset, choices };
}
