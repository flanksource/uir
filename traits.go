package uir

type Trait string

const (
	TraitExternal     Trait = "external"
	TraitGenerated    Trait = "generated"
	TraitDeprecated   Trait = "deprecated"
	TraitThirdParty   Trait = "third_party"
	TraitAutoImported Trait = "auto_imported"
	TraitTestCode     Trait = "test_code"
	TraitInterface    Trait = "interface"
	TraitAbstract     Trait = "abstract"
	TraitPrivate      Trait = "private"
	TraitPublic       Trait = "public"
	TraitFinal        Trait = "final"
	TraitMutable      Trait = "mutable"
	TraitImmutable    Trait = "immutable"
)

type TraitMixin interface {
	GetTraits() []Trait
}
