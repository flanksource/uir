TASK ?= task
VERSION ?= dev
RUN_TASK = $(TASK) VERSION=$(VERSION)

.PHONY: help build binary install schema schema-check bindings bindings-python bindings-typescript bindings-java query-parser test test-python fmt wasm-check vet lint clean web-build fixture-corpus-env fixture-corpus fixture-history

help:
	$(RUN_TASK) --list

build binary install schema bindings query-parser test fmt vet lint clean:
	$(RUN_TASK) $@

schema-check:
	$(RUN_TASK) schema:check

bindings-python bindings-typescript bindings-java:
	$(RUN_TASK) $(subst bindings-,bindings:,$@)

test-python:
	$(RUN_TASK) test:python

wasm-check:
	$(RUN_TASK) wasm:check

web-build:
	$(RUN_TASK) web:build

fixture-corpus-env:
	$(RUN_TASK) fixture:corpus:env

fixture-corpus fixture-history:
	$(RUN_TASK) $(subst fixture-,fixture:,$@)
