# OSS packages

The Privatemode client code (proxy and app) is licensed under MIT so that users can integrate it into their products.
The `internal/oss` directory groups all packages that are required on the client side.
It's important that the client packages must not depend on any Privatemode packages outside `internal/oss` or external code that is not permissively licensed.
