# Registry Package

This package is an umbrella package containing sub-packages for API types with storage implementations.

Structure:

```
/registry/
  registry.go - contains shared registry type embedding *[k8s.io/apiserver/pkg/registry/generic/registry.Store]
  {api-group}/
    {api-type}/
      store.go - contains etcd storage implementation for {api-type}
      strategy.go - contains storage strategy implementation for {api-type}
      table.go - contains table conversion implementation for {api-type}
```
