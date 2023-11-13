# Operating

[![license](https://img.shields.io/github/license/KusionStack/operating.svg)](https://github.com/KusionStack/operating/blob/main/LICENSE)
[![Operating](https://github.com/KusionStack/operating/actions/workflows/release.yaml/badge.svg)](https://github.com/KusionStack/operating/actions/workflows/release.yaml)
[![GitHub release](https://img.shields.io/github/release/KusionStack/operating.svg)](https://github.com/KusionStack/operating/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/KusionStack/operating)](https://goreportcard.com/report/github.com/KusionStack/operating)
[![codecov](https://codecov.io/gh/KusionStack/operating/branch/master/graph/badge.svg)](https://codecov.io/gh/KusionStack/operating)

KusionStack Operating ([official site](https://kusionstack.io/docs/operating/introduction/)) provides a set of workloads and operators
built on Kubernetes Custom Resource Definitions (CRDs), with a primary aim of bridging the gap 
between platform development and Kubernetes.

## Key features

KusionStack Operating currently provides the following features,
streamlining application operations when developing platforms based on Kubernetes:

* Fine-grained operation

[**PodOpsLifecycle**](https://kusionstack.io/docs/operating/concepts/podopslifecycle) 
extends native Pod lifecycle with more phase, like `PreCheck`, `Preparing`, `PostCheck`, to allow fine-grained operation management. 

* Advanced workloads

[**CollaSet**](https://kusionstack.io/docs/operating/manuals/collaset) designed to manage Pods with respect to PodOpsLifecycle.

* Streamlined pod operation

[**ResourceConsist**](https://kusionstack.io/docs/operating/manuals/resourceconsist) framework offers 
a graceful way to integrate resource management around Pods, like traffic control, into PodOpsLifecycle.

* Risk management

[**PodTransitionRule**](https://kusionstack.io/docs/operating/manuals/podtransitionrule) 
is responsible to keep Pod operation risks under control.

## Getting started

### Installation

You can install Operating following [installation doc](https://kusionstack.io/docs/operating/started/install).

### Tutorial

Please visit this [tutorial](https://kusionstack.io/docs/operating/started/demo-graceful-operation) to gracefully operate an application.

Alternatively, this [video](https://www.bilibili.com/video/BV1n8411q7sP/?t=15.7) also records the e2e experience.

## Contact us
- Twitter: [KusionStack](https://twitter.com/KusionStack)
- Slack: [Kusionstack](https://join.slack.com/t/kusionstack/shared_invite/zt-19lqcc3a9-_kTNwagaT5qwBE~my5Lnxg)
- DingTalk (Chinese): 42753001
- Wechat Group (Chinese)

  <img src="docs/wx_spark.jpg" width="200" height="200"/>

## 🎖︎ Contribution guide

KusionStack Operating is currently in its early stages. Our goal is to simplify platform development. 
We will continue building in areas such as application operations, observability, and insight.
We welcome everyone to participate in construction with us. Visit the [Contribution Guide](docs/contributing.md) 
to understand how to participate in the contribution KusionStack project. 
If you have any questions, please [Submit the Issue](https://github.com/KusionStack/operating/issues).