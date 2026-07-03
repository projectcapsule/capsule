@Library('libpipelines') _

hose {
    EMAIL = 'platform@stratio.com'
    BUILDTOOL_IMAGE = 'golang:1.26.4'
    BUILDTOOL = 'make'
    DEVTIMEOUT = 30
    ANCHORE_POLICY = "production"
    VERSIONING_TYPE = 'stratioVersion-3-3'
    UPSTREAM_VERSION = '0.10.3'
    DEPLOYONPRS = true
    GRYPE_TEST = true
    VERSIONING_TYPE = "semver"

    DEV = { config ->
        doDocker(conf:config, image:'capsule')
        doHelmChart(conf: config, helmTarget: "chart")
        doPushDockerECR(conf: config,AWS_CREDENTIALS_ID: 'AWS_CREDENTIALS_ECR_TEST',AWS_REGION: 'us-east-1')
        doPushHelmECR(conf: config,AWS_CREDENTIALS_ID: 'AWS_CREDENTIALS_ECR_TEST',AWS_REGION: 'us-east-1')
    }
}
