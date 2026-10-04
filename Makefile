TASK ?= task

.PHONY: help build binary install schema schema-check bindings bindings-python bindings-typescript bindings-java query-parser test test-python fmt wasm-check vet lint clean web-build fixture-corpus-env fixture-corpus fixture-history

help:
	$(TASK) --list

build binary install schema bindings query-parser test fmt vet lint clean:
	$(TASK) $@

schema-check:
	$(TASK) schema:check

bindings-python bindings-typescript bindings-java:
	$(TASK) $(subst bindings-,bindings:,$@)

test-python:
	$(TASK) test:python

wasm-check:
	$(TASK) wasm:check

web-build:
	$(TASK) web:build

fixture-corpus-env:
	$(TASK) fixture:corpus:env

fixture-corpus fixture-history:
	$(TASK) $(subst fixture-,fixture:,$@)
