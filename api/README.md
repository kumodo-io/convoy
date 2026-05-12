# API Package

This package is an umbrella package for all API types introduced in the project.

Structure:

```
/api/
  {api-group}/
    {api-version}/
      doc.go
      register.go
      {type-name}_types.go
    install.go - contains API types installation to scheme
    validation.go - contains validation logic for API types
    register.go - contains API types registration
    doc.go - contains internal package documentation
    types.go - contains unversioned API types definitions
  generated/
    applyconfigurations/
    clientset/
    informers/
    listers/
    openapi/
```
