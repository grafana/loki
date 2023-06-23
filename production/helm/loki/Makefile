.DEFAULT_GOAL := all
.PHONY: lint lint-yaml

lint: lint-yaml

lint-yaml:
	yamllint -c $(CURDIR)/src/.yamllint.yaml $(CURDIR)/src
