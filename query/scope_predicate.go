package query

// scopePredicate is a pure scope expression, a mod: or pkg: selector or a union of them, read as a test
// on a symbol instead of a set of symbols. Such a selector matches exactly the symbols defined in their
// own module's root whose module key (mod:) or package path (pkg:) its pattern accepts, so a symbol
// declared in a root satisfies it when its module key is that root's and one term accepts it. A
// relation or intersection filtered by it then never has to list the scope's symbols.
type scopePredicate struct {
	terms []scopeTerm
}

type scopeTerm struct {
	selector   Selector
	glob       selectorGlob
	moduleGlob selectorGlob
}

// compileScopePredicate reads expression as a scope predicate, reporting false when it is anything but
// mod: and pkg: selectors joined by unions.
func compileScopePredicate(expression *Expr) (scopePredicate, bool, error) {
	switch {
	case expression == nil:
		return scopePredicate{}, false, nil
	case expression.Kind == ExprSelector && (expression.Selector.Kind == "mod" || expression.Selector.Kind == "pkg"):
		term := scopeTerm{selector: *expression.Selector}
		var err error
		if term.glob, err = compileSelectorGlob(term.selector.Pattern); err != nil {
			return scopePredicate{}, false, err
		}
		if term.selector.ModulePattern != "" {
			if term.moduleGlob, err = compileSelectorGlob(term.selector.ModulePattern); err != nil {
				return scopePredicate{}, false, err
			}
		}
		return scopePredicate{terms: []scopeTerm{term}}, true, nil
	case expression.Kind == ExprUnion:
		left, scoped, err := compileScopePredicate(expression.Left)
		if !scoped || err != nil {
			return scopePredicate{}, false, err
		}
		right, scoped, err := compileScopePredicate(expression.Right)
		if !scoped || err != nil {
			return scopePredicate{}, false, err
		}
		return scopePredicate{terms: append(left.terms, right.terms...)}, true, nil
	}
	return scopePredicate{}, false, nil
}

// admitsRoot reports whether a symbol of the root with this key can satisfy the predicate.
func (predicate scopePredicate) admitsRoot(rootKey string) bool {
	for _, term := range predicate.terms {
		if term.admitsRoot(rootKey) {
			return true
		}
	}
	return false
}

func (term scopeTerm) admitsRoot(rootKey string) bool {
	if term.selector.Kind == "mod" {
		return term.glob.matches(rootKey)
	}
	return term.selector.ModulePattern == "" || term.moduleGlob.matches(rootKey)
}

// admits reports whether a symbol of module moduleKey and package packagePath, declared in the root
// with key rootKey, satisfies the predicate.
func (predicate scopePredicate) admits(rootKey, moduleKey, packagePath string) bool {
	return moduleKey == rootKey && predicate.admitsPackage(packagePath, moduleKey)
}

// admitsPackage reports whether a symbol of the package, defined in the root of its module, satisfies
// the predicate.
func (predicate scopePredicate) admitsPackage(packagePath, moduleKey string) bool {
	for _, term := range predicate.terms {
		if term.admitsRoot(moduleKey) && (term.selector.Kind == "mod" || packageFilter(term.selector, term.glob)(packagePath, moduleKey)) {
			return true
		}
	}
	return false
}

// packagesOnly reports whether every term is a pkg: selector, so the predicate narrows a module to the
// handle ranges of the packages it admits.
func (predicate scopePredicate) packagesOnly() bool {
	for _, term := range predicate.terms {
		if term.selector.Kind != "pkg" {
			return false
		}
	}
	return true
}
