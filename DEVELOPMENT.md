# Development

In Progress!


## Helm Chart


### Testing

You can test your changes made to the helm chart locally. They are almost identical to the checks executed in the github workflows.

Run chart linting (ct lint):

```bash
make helm-lint
```

Run chart tests (ct install):

```bash
make helm-test
```

### Documentation

Documentation of the chart is done with [helm-docs](https://github.com/norwoodj/helm-docs). Therefor all documentation relevant changes for the chart must be done in the [README.md.gotmpl](./charts/capsule/README.md.gotmpl) file. You can run this locally with this command (requires running docker daemon):

```bash
make helm-docs

...

time="2023-10-23T13:45:08Z" level=info msg="Found Chart directories [charts/capsule]"
time="2023-10-23T13:45:08Z" level=info msg="Generating README Documentation for chart /helm-docs/charts/capsule"
```

This will update the documentation for the chart in the `README.md` file. 

### Helm Changelog 

The `version` of the chart does not require a bump, since it's driven by our release process. The `appVersion` of the chart is the version of the Capsule project. This is the version that should be bumped when a new Capsule version is released. This will be done by the maintainers.

To create the proper changelog for the helm chart, all changes which affect the helm chart must be documented as chart annotation. See all the available [chart annotations](https://artifacthub.io/docs/topics/annotations/helm/).

This annotation can be provided using two different formats: using a plain list of strings with the description of the change or using a list of objects with some extra structured information (see example below). Please feel free to use the one that better suits your needs. The UI experience will be slightly different depending on the choice. When using the list of objects option the valid supported kinds are `added`, `changed`, `deprecated`, `removed`, `fixed` and `security`.