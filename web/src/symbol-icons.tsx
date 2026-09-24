import { UiAnnotation, UiClass3, UiClass6, UiColumn, UiConstant1, UiField3, UiFunction1, UiGroupByPackage, UiIndex, UiMethod2, UiMethod6, UiModule1, UiRecord1, UiSymbol, UiVariable6 } from "@flanksource/clicky-ui/icons";

const symbolIcons = {
  package: UiGroupByPackage,
  module: UiModule1,
  class: UiClass3,
  interface: UiClass6,
  record: UiRecord1,
  method: UiMethod6,
  function: UiFunction1,
  constructor: UiMethod2,
  record_field: UiField3,
  column: UiColumn,
  index: UiIndex,
  function_variable: UiVariable6,
  package_variable: UiVariable6,
  annotation: UiAnnotation,
  constant: UiConstant1,
} as const;

export function symbolIcon(nodeType: string) {
  return symbolIcons[nodeType as keyof typeof symbolIcons] ?? UiSymbol;
}

export function SymbolIcon({ nodeType }: { nodeType: string }) {
  const Icon = symbolIcon(nodeType);
  return <Icon className="size-4 shrink-0" aria-hidden="true" />;
}
