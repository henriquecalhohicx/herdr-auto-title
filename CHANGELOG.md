# Changelog

## [0.1.1](https://github.com/kryptamine/herdr-auto-title/compare/v0.1.0...v0.1.1) (2026-08-25)


### Bug Fixes

* **resolver:** cut a kind by the length that actually matched ([99361ab](https://github.com/kryptamine/herdr-auto-title/commit/99361ab712149c6e7a308591d90092e8746eaae7))
* **resolver:** strip the invisible characters that forge a label ([9e96e14](https://github.com/kryptamine/herdr-auto-title/commit/9e96e14d85ba462b3a8e6262e19636d5e0b323a1))


### Performance

* **app:** reuse a process read instead of making one per pane per poll ([d2595a7](https://github.com/kryptamine/herdr-auto-title/commit/d2595a786ed6df10029ecf3b8243ba2d1ae87032))


### Refactoring

* **herdr:** drop the snapshot fields nothing reads ([a3de7d5](https://github.com/kryptamine/herdr-auto-title/commit/a3de7d51918b7c73be61797c3ed1ac5c608cfcfb))
* **resolver:** make every source decline a nil pane alike ([2f2a036](https://github.com/kryptamine/herdr-auto-title/commit/2f2a03686be02f91579f73bf4158b462446011e2))
* sort through slices rather than sort ([edda7fa](https://github.com/kryptamine/herdr-auto-title/commit/edda7fa675efacc05f1448a81b1318aa929f5691))
* **state:** let encoding/json sort the manual-name file ([75cbf5c](https://github.com/kryptamine/herdr-auto-title/commit/75cbf5c62e51ceb2a92e50593486743fd5b4b6ce))
