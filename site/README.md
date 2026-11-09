# taxcalcs documentation site

A static site that explains how the rules compute the 2025 federal return and
California's, for readers who know tax rather than code. It is an Angular
application, prerendered to one HTML page per source, anchor, rule, figure
set, cited section of law and form.

It is its own Bazel module, and builds hermetically. Node comes from
`rules_nodejs`, and every npm package from `pnpm-lock.yaml` through
`rules_js`, so nothing on the machine is used but Bazel. Its inputs come
from the repository, which this module depends on through a
`local_path_override`:

- `//docsmodel:model`, generated from the source catalog, the taxlisp
  rules and the sample households' derivations;
- `//sources/pdfs`, the vendored documents, served under `/pdfs`.

From this directory:

```bash
bazel build //:site
bazel run //:serve
```

`//:site` is the static site, under `bazel-bin/dist/site/browser`.
`//:serve` serves it at http://localhost:4300.

After changing `package.json`, update the lockfile with the same pnpm:

```bash
bazel run -- @pnpm --dir "$PWD" install --lockfile-only
```

The root module ignores this directory, so the root `bazel test //...` does
not need Node.
