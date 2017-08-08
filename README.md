# Summary

This is the prototype of Cloud Foundry Firehose nozzle for Application Insights. It forwards [app level logs](https://docs.cloudfoundry.org/devguide/deploy-apps/streaming-logs.html) from Firehose to Application Insights.
* RTR logs are parsed and tracked as HTTP Requests
* Other logs are tracked as Traces

# Prerequisites
### 1. Deploy a CF or PCF environment in Azure

* [Deploy Cloud Foundry on Azure](https://github.com/cloudfoundry-incubator/bosh-azure-cpi-release/blob/master/docs/guidance.md)
* [Deploy Pivotal Cloud Foundry on Azure](https://docs.pivotal.io/pivotalcf/1-9/customizing/azure.html)

### 2. Install CLIs on your dev box

* [Install Cloud Foundry CLI](https://github.com/cloudfoundry/cli#downloads)
* [Install Cloud Foundry UAA Command Line Client](https://github.com/cloudfoundry/cf-uaac/blob/master/README.md)

### 3. Create an Application Insights resource in Azure

* [Create Application Insights resource](https://docs.microsoft.com/en-us/azure/application-insights/app-insights-create-new-resource)

# Deploy - Push the Nozzle as an App to Cloud Foundry
### 1. Utilize the CF CLI to authenticate with your CF instance
```
cf login -a https://api.${ENDPOINT} -u ${CF_USER} --skip-ssl-validation
```

### 2. Create a CF user and grant required privileges
The Log Analytics Nozzle requires a CF user who is authorized to access the loggregator firehose.
```
uaac target https://uaa.${ENDPOINT} --skip-ssl-validation
uaac token client get admin
cf create-user ${FIREHOSE_USER} ${FIREHOSE_USER_PASSWORD}
uaac member add cloud_controller.admin ${FIREHOSE_USER}
uaac member add doppler.firehose ${FIREHOSE_USER}
```

### 3. Download the latest code
```
git clone https://github.com/lizzha/application-insights-firehose-nozzle.git
cd application-insights-firehose-nozzle
```

### 4. Set environment variables in [manifest.yml](./manifest.yml)
```
INSTRUMENT_KEY            : The instrument key of Application Insights
API_ADDR                  : The api URL of the CF environment
DOPPLER_ADDR              : Loggregator's traffic controller URL
FIREHOSE_USER             : CF user who has admin and firehose access
FIREHOSE_USER_PASSWORD    : Password of the CF user
SKIP_SSL_VALIDATION       : If true, allows insecure connections to the UAA and the Trafficcontroller
IDLE_TIMEOUT              : Keep Alive duration for the firehose consumer
LOG_LEVEL                 : Logging level of the nozzle, valid levels: DEBUG, INFO, ERROR
```


### 5. Push the app
```
cf push
```
