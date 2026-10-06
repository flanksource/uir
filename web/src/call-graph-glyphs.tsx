// The icons the call graph uses in place of words. call-graph-labels.ts decides which glyph a node
// gets; this file only draws it. The coloured artwork comes in a light and a dark drawing, picked by
// the resolved theme.
import { useResolvedTheme } from "@flanksource/clicky-ui/hooks";
import {
  UiArrowRight, UiConstant, UiField, UiFunction, UiFunction1, UiFunction1Dark, UiFunctionSquare, UiInterface, UiMethod,
  UiMethod11, UiMethod11Dark, UiPackage, UiShowToImplement, UiShowToImplementDark, UiSymbol, UiSymbolDark, UiTypeParameter,
  UiUnknown, UiVariable, type IconComponent, type IconProps,
} from "@flanksource/clicky-ui/icons";
import type { NodeGlyph } from "./call-graph-labels";

interface ThemedIcon {
  light: IconComponent;
  dark?: IconComponent;
}

const NODE_ICON: Record<NodeGlyph, ThemedIcon> = {
  func: { light: UiFunction },
  method: { light: UiMethod },
  interface_method: { light: UiInterface },
  recursive: { light: UiMethod11, dark: UiMethod11Dark },
  external_func: { light: UiFunction1, dark: UiFunction1Dark },
  builtin: { light: UiFunctionSquare },
  type: { light: UiTypeParameter },
  field: { light: UiField },
  var: { light: UiVariable },
  const: { light: UiConstant },
  package: { light: UiPackage },
  unresolved: { light: UiUnknown },
  symbol: { light: UiSymbol, dark: UiSymbolDark },
};

const DISPATCH_ICON: ThemedIcon = { light: UiShowToImplement, dark: UiShowToImplementDark };

function Themed({ icon, ...props }: IconProps & { icon: ThemedIcon }) {
  const Icon = useResolvedTheme() === "dark" && icon.dark ? icon.dark : icon.light;
  return <Icon {...props} />;
}

export function NodeGlyphIcon({ glyph, ...props }: IconProps & { glyph: NodeGlyph }) {
  return <Themed icon={NODE_ICON[glyph]} {...props} />;
}

/** An edge that reaches an implementation through an interface method. */
export function DispatchIcon(props: IconProps) {
  return <Themed icon={DISPATCH_ICON} {...props} />;
}

/** A direct call: the plain arrow the graph draws for it. */
export function CallIcon(props: IconProps) {
  return <UiArrowRight {...props} />;
}

export function PackageIcon(props: IconProps) {
  return <UiPackage {...props} />;
}

/** A group box caption: the package icon, then the shortened path, kept inline so the caption still truncates. */
export function PackageCaption({ caption }: { caption: string }) {
  return <><UiPackage className="mr-1 inline-block align-[-0.15em]" />{caption}</>;
}
