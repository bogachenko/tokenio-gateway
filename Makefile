git:
	git add .
	git commit -a -m "$m"
	git push -u origin main

.PHONY: openapi openapi-check

openapi:
	bash scripts/openapi_check.sh

openapi-check: openapi
