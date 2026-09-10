# Vendored Swagger UI

`assets/` holds a trimmed copy of [`swagger-ui-dist`](https://www.npmjs.com/package/swagger-ui-dist)
**v5.32.15**, vendored (not npm-installed) so `GET /docs` works with no
outbound network access at runtime — consistent with this project's
single-binary deploy model. `index.html` is hand-written for Sevitout rather
than the package's own `index.html`/`swagger-initializer.js`, pointed at this
server's `/openapi.json` instead of the Petstore demo spec.

Only the runtime bundle is kept — `swagger-ui-bundle.js`,
`swagger-ui-standalone-preset.js`, `swagger-ui.css`, and the two favicons.
Source maps, the ES-module bundles, and everything else in the npm package
are dev-time extras this server never serves, so they're dropped to keep the
embedded binary smaller.

To upgrade: download the new tarball, diff `assets/` against the packed
`package/` directory, and copy over just those five files.

```sh
npm view swagger-ui-dist version dist.tarball
curl -sL <tarball> | tar xz
cp package/{swagger-ui-bundle.js,swagger-ui-standalone-preset.js,swagger-ui.css,favicon-16x16.png,favicon-32x32.png} assets/
cp package/LICENSE package/NOTICE .
```

Licensed Apache-2.0 by the SmartBear/swagger-api project — see `LICENSE` and
`NOTICE`.
