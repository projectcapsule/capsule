package workloads

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestGetPodQoSClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pod  *corev1.Pod
		want corev1.PodQOSClass
	}{
		{
			name: "nil pod returns BestEffort",
			pod:  nil,
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "status QoS class takes precedence over computed value",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					QOSClass: corev1.PodQOSGuaranteed,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "empty status computes QoS class",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "pod-level Guaranteed takes precedence over BestEffort containers",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: guaranteedPodResourcesPtr("100m", "128Mi"),
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "pod-level Burstable takes precedence over Guaranteed containers",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: requestOnlyPodResourcesPtr("100m", "128Mi"),
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "BestEffort without pod-level or container-level resources",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "Burstable with container requests only",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						requestOnlyContainer("app", "100m", "128Mi"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "Burstable with container limits only",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						limitOnlyContainer("app", "100m", "128Mi"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "Guaranteed with equal CPU and memory requests and limits",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "Burstable when CPU request and limit differ",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						containerWithResources(
							"app",
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "Burstable when memory request and limit differ",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						containerWithResources(
							"app",
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "Burstable when one container is BestEffort",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
						bestEffortContainer("sidecar"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "Guaranteed with multiple guaranteed containers",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
						guaranteedContainer("sidecar", "50m", "64Mi"),
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "Burstable when init container has requests only",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						requestOnlyContainer("init", "100m", "128Mi"),
					},
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "Guaranteed when init and regular containers are guaranteed",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						guaranteedContainer("init", "100m", "128Mi"),
					},
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "Burstable when ephemeral container has requests only",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
					},
					EphemeralContainers: []corev1.EphemeralContainer{
						{
							EphemeralContainerCommon: corev1.EphemeralContainerCommon{
								Name:      "debug",
								Resources: requestOnlyRequirements("100m", "128Mi"),
							},
						},
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "Guaranteed when ephemeral container is guaranteed",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
					},
					EphemeralContainers: []corev1.EphemeralContainer{
						{
							EphemeralContainerCommon: corev1.EphemeralContainerCommon{
								Name:      "debug",
								Resources: guaranteedRequirements("100m", "128Mi"),
							},
						},
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "zero CPU and memory requests and limits are ignored",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						containerWithResources(
							"app",
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("0"),
								corev1.ResourceMemory: resource.MustParse("0"),
							},
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("0"),
								corev1.ResourceMemory: resource.MustParse("0"),
							},
						),
					},
				},
			},
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "unsupported resources do not influence QoS",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						containerWithResources(
							"app",
							corev1.ResourceList{
								corev1.ResourceName("example.com/gpu"): resource.MustParse("1"),
							},
							corev1.ResourceList{
								corev1.ResourceName("example.com/gpu"): resource.MustParse("1"),
							},
						),
					},
				},
			},
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "hugepages do not influence QoS",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						containerWithResources(
							"app",
							corev1.ResourceList{
								corev1.ResourceName("hugepages-2Mi"): resource.MustParse("2Mi"),
							},
							corev1.ResourceList{
								corev1.ResourceName("hugepages-2Mi"): resource.MustParse("2Mi"),
							},
						),
					},
				},
			},
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "pod-level unsupported resources do not influence QoS",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("example.com/gpu"): resource.MustParse("1"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceName("example.com/gpu"): resource.MustParse("1"),
						},
					},
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "pod-level zero resources do not mask container-level Guaranteed",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
					},
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "pod-level CPU only request is Burstable",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "pod-level memory only limit is Burstable",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "pod-level CPU and memory unequal request and limit is Burstable",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
					Containers: []corev1.Container{
						bestEffortContainer("app"),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
		{
			name: "no containers and no resources is BestEffort",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{},
			},
			want: corev1.PodQOSBestEffort,
		},
		{
			name: "aggregate container requests and limits equal returns Guaranteed",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						guaranteedContainer("app", "100m", "128Mi"),
						guaranteedContainer("sidecar", "200m", "256Mi"),
					},
				},
			},
			want: corev1.PodQOSGuaranteed,
		},
		{
			name: "mismatched container requests/limits are Burstable even if totals match",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						containerWithResources(
							"a",
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						),
						containerWithResources(
							"b",
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						),
					},
				},
			},
			want: corev1.PodQOSBurstable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := GetPodQoSClass(tt.pod)
			if got != tt.want {
				t.Fatalf("expected QoS class %q, got %q", tt.want, got)
			}
		})
	}
}

func TestComputePodLevelQoSClassNilPodResources(t *testing.T) {
	t.Parallel()

	got, ok := computePodLevelQoSClass(&corev1.Pod{
		Spec: corev1.PodSpec{
			Resources: nil,
		},
	})

	if got != corev1.PodQOSBestEffort {
		t.Fatalf("expected QoS class %q, got %q", corev1.PodQOSBestEffort, got)
	}

	if ok {
		t.Fatalf("expected ok=false, got true")
	}
}

func TestComputePodLevelQoSClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pod  *corev1.Pod
		want corev1.PodQOSClass
		ok   bool
	}{
		{
			name: "nil pod returns false",
			pod:  nil,
			want: corev1.PodQOSBestEffort,
			ok:   false,
		},
		{
			name: "no pod-level resources returns false",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{},
			},
			want: corev1.PodQOSBestEffort,
			ok:   false,
		},
		{
			name: "pod-level Guaranteed",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: guaranteedPodResourcesPtr("100m", "128Mi"),
				},
			},
			want: corev1.PodQOSGuaranteed,
			ok:   true,
		},
		{
			name: "pod-level request only is Burstable",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: requestOnlyPodResourcesPtr("100m", "128Mi"),
				},
			},
			want: corev1.PodQOSBurstable,
			ok:   true,
		},
		{
			name: "pod-level limit only is Burstable",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: limitOnlyPodResourcesPtr("100m", "128Mi"),
				},
			},
			want: corev1.PodQOSBurstable,
			ok:   true,
		},
		{
			name: "pod-level CPU only equal request and limit is Burstable",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("100m"),
						},
					},
				},
			},
			want: corev1.PodQOSBurstable,
			ok:   true,
		},
		{
			name: "pod-level memory only equal request and limit is Burstable",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
			want: corev1.PodQOSBurstable,
			ok:   true,
		},
		{
			name: "pod-level zero resources return false",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("0"),
							corev1.ResourceMemory: resource.MustParse("0"),
						},
					},
				},
			},
			want: corev1.PodQOSBestEffort,
			ok:   false,
		},
		{
			name: "pod-level unsupported resources return false",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Resources: &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceName("example.com/gpu"): resource.MustParse("1"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceName("example.com/gpu"): resource.MustParse("1"),
						},
					},
				},
			},
			want: corev1.PodQOSBestEffort,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := computePodLevelQoSClass(tt.pod)
			if got != tt.want {
				t.Fatalf("expected QoS class %q, got %q", tt.want, got)
			}

			if ok != tt.ok {
				t.Fatalf("expected ok=%t, got %t", tt.ok, ok)
			}
		})
	}
}

func TestQoSHelpers(t *testing.T) {
	t.Parallel()

	t.Run("hasSupportedQoSResource", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			resources corev1.ResourceList
			want      bool
		}{
			{
				name:      "nil resources",
				resources: nil,
				want:      false,
			},
			{
				name:      "empty resources",
				resources: corev1.ResourceList{},
				want:      false,
			},
			{
				name: "CPU positive",
				resources: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
				want: true,
			},
			{
				name: "memory positive",
				resources: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Mi"),
				},
				want: true,
			},
			{
				name: "CPU zero",
				resources: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("0"),
				},
				want: false,
			},
			{
				name: "unsupported positive",
				resources: corev1.ResourceList{
					corev1.ResourceName("example.com/gpu"): resource.MustParse("1"),
				},
				want: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				got := hasSupportedQoSResource(tt.resources)
				if got != tt.want {
					t.Fatalf("expected %t, got %t", tt.want, got)
				}
			})
		}
	})

	t.Run("positiveResource", func(t *testing.T) {
		t.Parallel()

		resources := corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("0"),
		}

		if got, ok := positiveResource(resources, corev1.ResourceCPU); !ok || got.Cmp(resource.MustParse("100m")) != 0 {
			t.Fatalf("expected positive CPU resource, got %q ok=%t", got.String(), ok)
		}

		if got, ok := positiveResource(resources, corev1.ResourceMemory); ok {
			t.Fatalf("expected zero memory to be ignored, got %q ok=%t", got.String(), ok)
		}

		if got, ok := positiveResource(resources, corev1.ResourceStorage); ok {
			t.Fatalf("expected missing storage to be ignored, got %q ok=%t", got.String(), ok)
		}
	})

	t.Run("addResource", func(t *testing.T) {
		t.Parallel()

		resources := corev1.ResourceList{}

		addResource(resources, corev1.ResourceCPU, resource.MustParse("100m"))
		addResource(resources, corev1.ResourceCPU, resource.MustParse("250m"))

		got := resources[corev1.ResourceCPU]
		want := resource.MustParse("350m")

		if got.Cmp(want) != 0 {
			t.Fatalf("expected accumulated CPU %q, got %q", want.String(), got.String())
		}
	})

	t.Run("isSupportedQoSComputeResource", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			in   corev1.ResourceName
			want bool
		}{
			{name: "CPU", in: corev1.ResourceCPU, want: true},
			{name: "memory", in: corev1.ResourceMemory, want: true},
			{name: "storage", in: corev1.ResourceStorage, want: false},
			{name: "ephemeral storage", in: corev1.ResourceEphemeralStorage, want: false},
			{name: "extended resource", in: corev1.ResourceName("example.com/gpu"), want: false},
			{name: "hugepage", in: corev1.ResourceName("hugepages-2Mi"), want: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				got := isSupportedQoSComputeResource(tt.in)
				if got != tt.want {
					t.Fatalf("expected %t, got %t", tt.want, got)
				}
			})
		}
	})
}

func bestEffortContainer(name string) corev1.Container {
	return corev1.Container{Name: name}
}

func requestOnlyContainer(name, cpu, memory string) corev1.Container {
	return corev1.Container{
		Name:      name,
		Resources: requestOnlyRequirements(cpu, memory),
	}
}

func limitOnlyContainer(name, cpu, memory string) corev1.Container {
	return corev1.Container{
		Name:      name,
		Resources: limitOnlyRequirements(cpu, memory),
	}
}

func guaranteedContainer(name, cpu, memory string) corev1.Container {
	return corev1.Container{
		Name:      name,
		Resources: guaranteedRequirements(cpu, memory),
	}
}

func containerWithResources(
	name string,
	requests corev1.ResourceList,
	limits corev1.ResourceList,
) corev1.Container {
	return corev1.Container{
		Name: name,
		Resources: corev1.ResourceRequirements{
			Requests: requests,
			Limits:   limits,
		},
	}
}

func requestOnlyRequirements(cpu, memory string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(memory),
		},
	}
}

func limitOnlyRequirements(cpu, memory string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(memory),
		},
	}
}

func guaranteedRequirements(cpu, memory string) corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(memory),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(cpu),
			corev1.ResourceMemory: resource.MustParse(memory),
		},
	}
}

func requestOnlyPodResources(cpu, memory string) corev1.ResourceRequirements {
	return requestOnlyRequirements(cpu, memory)
}

func limitOnlyPodResources(cpu, memory string) corev1.ResourceRequirements {
	return limitOnlyRequirements(cpu, memory)
}

func guaranteedPodResources(cpu, memory string) corev1.ResourceRequirements {
	return guaranteedRequirements(cpu, memory)
}

func requestOnlyPodResourcesPtr(cpu, memory string) *corev1.ResourceRequirements {
	resources := requestOnlyRequirements(cpu, memory)

	return &resources
}

func limitOnlyPodResourcesPtr(cpu, memory string) *corev1.ResourceRequirements {
	resources := limitOnlyRequirements(cpu, memory)

	return &resources
}

func guaranteedPodResourcesPtr(cpu, memory string) *corev1.ResourceRequirements {
	resources := guaranteedRequirements(cpu, memory)

	return &resources
}
