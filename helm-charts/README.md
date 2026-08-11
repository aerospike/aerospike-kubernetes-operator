# Aerospike Helm Charts

Development and contribution notes for the charts in this directory. For end-user installation and
configuration docs, see each chart's own README (e.g.
[aerospike-kubernetes-operator](aerospike-kubernetes-operator/README.md)).

## Values validation

`values.yaml` is validated against `values.schema.json` by Helm on every `install`/`upgrade`/`template`, so bad values fail fast with a pointer to the offending field instead of producing a broken manifest.

**`values.schema.json` is generated - do not edit it by hand.** It is built from `values.yaml` by the
[`helm-values-schema-json`](https://github.com/losisin/helm-values-schema-json) plugin. Validation rules live in
`# @schema ...` comments in `values.yaml`, field descriptions in `# -- ...` comments, and generator settings in
`.schema.yaml`. Anything written directly into the JSON is lost the next time the generator runs.

### Adding or changing a value

1. Add the key to `values.yaml` with a `# -- ` description, plus any `# @schema` constraints
   (see the [annotation reference](https://github.com/losisin/helm-values-schema-json/blob/main/docs/README.md)).
2. Regenerate and commit the schema from the repo root:

   ```sh
   make helm-schema
   ```

   CI fails the PR when the committed schema is stale.

3. Verify with `helm schema lint --strict`, `helm lint .`, and `make helm-test` (test suites live in `tests/`).

**Note:**

- Kubernetes passthrough objects (`affinity`, `nodeSelector`, `podSecurityContext`, `resources`, …) are intentionally left open so arbitrary upstream fields are accepted.
