Examples added to this page will be appended to the Loki documentation.
We distinguish between two types of examples:
- Examples: These should be ready to run.
- Snippets: These are code snippets illustrating how to configure a specific feature.
            They are not ready to run and require additional configuration.

Once an example is added to this directory, run the following command to update the documentation:
```bash
make generate-example-config-doc
```

Additionally, you can run the following command to verify that the example is valid.
Note that validation is skipped for those examples containing the `# doc-example:skip-validation=true` comment.
```bash
make validate-example-configs
```
