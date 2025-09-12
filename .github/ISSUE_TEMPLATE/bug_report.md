---
name: Bug report
about: Create a report to help us improve
title: yaml with cue the result is not correct
labels: NeedsInvestigation, Triage
assignees: ''

---

<!--
Please answer these questions before submitting your issue. Thanks!
To ask questions, see https://github.com/cue-lang/cue#contact.
-->

### What version of CUE are you using (`cue version`)?

<pre>
$ cue version
cue version v0.14.1

go version go1.24.6
      -buildmode exe
       -compiler gc
       -trimpath true
     CGO_ENABLED 0
          GOARCH amd64
            GOOS darwin
         GOAMD64 v1
cue.lang.version v0.14.1

</pre>

### Does this issue reproduce with the latest stable release?

This problem can be reproduced

### What did you do?

first i create a yaml name instance.yaml
```
spec:
  name: "Alibaba"
  technology_id: '5002'
  external_id: "technology##5002"
  category: "data_asset"
  sub_category: "database_servers"
  description: "ApsaraDB RDS 是一款由阿里云提供的稳定、可靠且可扩展的在线数据库服务"
  resource_native_type: "aliyun"
  website: "https://www.alibabacloud.com/help/en/apsaradb-for-rds/latest/quick-start-create-an-apsaradb-rds-for-mysql-instance"
  tech_status: "unapproved"
  cloud_platform: "aliyun"
  tech_type: "cloud_platform_service"
  owner_name: "Alibaba Group Holding Limited"
  cloud_service: 1

```
second i create a cue name schema.cue
```
#TechnologyMetadata:
		name:                 string
		technology_id:        string
		external_id:          string
		category:             string
		sub_category:         string
		description:          string
		resource_native_type: string
		website:              string
		tech_status:          "approved" | "unapproved"
		cloud_platform:       string
		tech_type:            string
		owner_name:           string
		cloud_service:        int

````
finally execute command 
```
cue vet -c schema.cue instance.yaml
```



### What did you expect to see?
I want to see no output



### What did you see instead?
but the result is 
```
❯ cue vet -c schema.cue instance.yaml

category: incomplete value string:
    ./schema.cue:5:25
cloud_platform: incomplete value string:
    ./schema.cue:11:25
cloud_service: incomplete value int:
    ./schema.cue:14:25
description: incomplete value string:
    ./schema.cue:7:25
external_id: incomplete value string:
    ./schema.cue:4:25
owner_name: incomplete value string:
    ./schema.cue:13:25
resource_native_type: incomplete value string:
    ./schema.cue:8:25
sub_category: incomplete value string:
    ./schema.cue:6:25
tech_status: incomplete value "approved" | "unapproved"
tech_type: incomplete value string:
    ./schema.cue:12:25
technology_id: incomplete value string:
    ./schema.cue:3:25
website: incomplete value string:
    ./schema.cue:9:25

```
