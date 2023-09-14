## Summary

Kubernetes provides a set of default controllers for workload management, like StatefulSet, Deployment, DaemonSet for instances. While at the same time, when a pod managed by this controllers undergoes changes, if the traffic is not completely stopped, it will result in some degree of traffic loss.

PodOpsLifecycle attempts to provide Kubernetes administrators and developers with finer-grained control the entire lifecycle of a pod. For example, we can develop a controller to do some necessary things in both the trafficoff and trafficon phases to avoid traffic loss.

## Goals

1. Provides extensibility that allows users to control the lifecycle of pods using the podopslifecycle mechanism.
2. Provide some concurrency, multi controllers can operate the pod in the same time. For example, when a pod is going to be replaced, other controllers may want to delete it.
3. All the lifecycle phases of a pod can be tracing.

## Design Details

1. Podopslifecycle mechanism is provided by a mutating webhook server and a controller. The mutating webhook server will chage the labels at the right time, and the controller will set the readinessgate `pod.kusionstack.io/service-ready` to true or false if necessary. The controller will also chage the label at some time.
2. The label `operating.podopslifecycle.kusionstack.io/<id>=<time>` and `operation-type.podopslifecycle.kusionstack.io/<id>=<type>` will be validated by a validating webhook server, they must be added or removed at the same time by the operation controller.
3. Service controller should turn the traffic on or off based on label `prepare.podopslifecycle.kusionstack.io/<id>=<time>` and `complete.podopslifecycle.kusionstack.io/<id>=<time>`.
4. Protection finalizer names must have prefix `prot.podopslifecycle.kusionstack.io`. They are used to determine whether the traffic has been completely removed or is fully prepared.
5. The special label `podopslifecycle.kusionstack.io/service-available` indicate a pod is available to serve.
6. We can use the message `<id>=<time>` and `<id>=<type>` in the labels to tracing a pod. The `<time>` is a unix time.

Below is the sequence diagram of podopslifecycle mechanism.

```mermaid
sequenceDiagram
	autonumber
	participant Operation Controller
	participant Pod
	participant PodOpsLifecycle
	participant PodTransitionRule
	participant Service Controller

	rect rgba(144,238,144,0.4)
		Operation Controller ->> Pod: Add operating.podopslifecycle.kusionstack.io/<id>=<time>, <br/>add operation-type.podopslifecycle.kusionstack.io/<id>=<type> at the same time.
		Note right of Pod: We can use operation-type.podopslifecycle.kusionstack.io to get the type of an id.
		PodOpsLifecycle ->> Pod: When operating.podopslifecycle.kusionstack.io/<id>=<time> added，<br/>add pre-check.podopslifecycle.kusionstack.io/<id>=<time>.
		PodTransitionRule ->> Pod: Once pre-check.podopslifecycle.kusionstack.io/<id>=<time> added，<br/>get the corresponding type based on the id, <br/>if all rules with condition=<type> passed，<br/>add pre-checked.podopslifecycle.kusionstack.io/<id>=<time>, <br/>and add operation-permission.podopslifecycle.kusionstack.io/<type>=<time>.
		Note right of PodTransitionRule: Operation of different types can be performed concurrently，<br/>and whether it can be performed concurrently with the same type <br/>is determined by user's operation controller.
	end

	rect rgba(255,222,173,0.4)
		PodOpsLifecycle ->> Pod: When pre-checked.podopslifecycle.kusionstack.io added, <br/>then add prepare.podopslifecycle.kusionstack.io/<id>=<time>, <br/>also set readinessgate pod.kusionstack.io/service-ready as false.
		Service Controller ->> Pod: When prepare.podopslifecycle.kusionstack.io added, then do some necessary things，<br/>remove the corresponding finalizer finally.
	end

	rect rgba(255,192,203,0.4)
		PodOpsLifecycle ->> Pod: When all protection finalizers have been removed，<br/>then add operate.podopslifecycle.kusionstack.io/<id>=<time>.
		alt
			Operation Controller ->> Pod: When operate.podopslifecycle.kusionstack.io added，<br/>do the operation like replacing pod, deleting pod，<br/>and then remove operating.podopslifecycle.kusionstack.io/<id>=<time>, <br/>also remove operation-type.podopslifecycle.kusionstack.io/<id>=<type>.
		else
			Operation Controller -->> Pod: If the controller want to cancel the operation，<br/>it should add undo-operation-type.podopslifecycle.kusionstack.io/<id>=<type>.
		end

		alt
			PodOpsLifecycle ->> Pod: When all the operating.podopslifecycle.kusionstack.io/<id>=<time> lables have been removed，<br/>then remove pre-check.podopslifecycle.kusionstack.io/<id>=<time>, <br/>and remove pre-checked.podopslifecycle.kusionstack.io/<id>=<time>, <br/>and remove operation-permission.podopslifecycle.kusionstack.io/<type>=<time>, <br/>and remove prepare.lifecycle/<id>=<time>, <br/>then add operated.podopslifecycle.kusionstack.io/<id>=<time>, <br/>and add done-operation-type.podopslifecycle.kusionstack.io/<id>=<type>.
			Note right of Pod: We can use done-operation-type.podopslifecycle.kusionstack.io to get the type of an id.
		else
			PodOpsLifecycle -->> Pod: When undo-operation-type.podopslifecycle.kusionstack.io/<id>=<type> added, <br/>then remove other labels with the same id.<br/>If all the operations with the same type have been canceled，<br/>remove operation-permission.podopslifecycle.kusionstack.io/<type>=<time>.
		end
	end

	rect rgba(255,222,173,0.4)
		PodOpsLifecycle ->> Pod: Once all the operated.podopslifecycle.kusionstack.io/<id>=<time> added，<br/>then add post-check.podopslifecycle.kusionstack.io/<id>=<time>.
		PodTransitionRule ->> Pod: When post-check.podopslifecycle.kusionstack.io/<id>=<time> added, <br/>then get the corresponding type based on the id.<br/>If all rules with condition=<type> passed，<br/>add post-checked.podopslifecycle.kusionstack.io/<id>=<time>.
	end

	rect rgba(144,238,144,0.4)
		PodOpsLifecycle ->> Pod: Once all the post-checked.podopslifecycle.kusionstack.io/<id>=<time> added，<br/>add complete.podopslifecycle.kusionstack.io/<id>=<time>, <br/>also set readinessgate pod.kusionstack.io/service-ready as false.
		Service Controller ->> Pod: When complete.podopslifecycle.kusionstack.io/<id>=<time> added, <br/>add the protection finalizer.
		PodOpsLifecycle ->> Pod: When all protection finalizers have been added，<br/>then remove operate.podopslifecycle.kusionstack.io/<id>=<type>, <br/>and remove done-operation-type.podopslifecycle.kusionstack.io/<id>=<type>, <br/>and remove post-check.podopslifecycle.kusionstack.io/<id>=<time>, <br/>and remove post-checked.podopslifecycle.kusionstack.io/<id>=<time>, <br/>and remove complete.podopslifecycle.kusionstack.io/<id>=<time>.<br/> If there is no label with id, add `podopslifecycle.kusionstack.io/service-available`.
	end
```