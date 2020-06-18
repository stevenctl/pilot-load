package app

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/howardjohn/pilot-load/pkg/simulation/model"
	"github.com/howardjohn/pilot-load/pkg/simulation/util"
	"github.com/howardjohn/pilot-load/pkg/simulation/xds"
)

type PodSpec struct {
	ServiceAccount string
	Node           string
	App            string
	Namespace      string
	UID            string
	IP             string
}

type Pod struct {
	Spec *PodSpec
	// For internal optimization around closing only
	created bool
	xds     *xds.Simulation
}

var _ model.Simulation = &Pod{}

func NewPod(s PodSpec) *Pod {
	if s.UID == "" {
		s.UID = util.GenUID()
	}
	if s.IP == "" {
		s.IP = util.GetIP()
	}
	return &Pod{
		Spec: &s,
	}
}

func (p *Pod) Run(ctx model.Context) (err error) {
	pod := p.getPod()
	// TODO apply gets pod stuck in pending state.. figure out how to force Running
	if err = ctx.Client.Apply(pod); err != nil {
		return fmt.Errorf("failed to apply config: %v", err)
	}

	p.created = true
	p.xds = &xds.Simulation{
		Labels:    pod.Labels,
		Namespace: pod.Namespace,
		Name:      pod.Name,
		IP:        p.Spec.IP,
		// TODO: multicluster
		Cluster: "pilot-load",
	}
	return p.xds.Run(ctx)
}

func (p *Pod) Cleanup(ctx model.Context) error {
	if p.created {
		if err := ctx.Client.Delete(p.getPod()); err != nil {
			return err
		}
	}
	return p.xds.Cleanup(ctx)
}

func (p *Pod) Name() string {
	return fmt.Sprintf("%s-%s", p.Spec.App, p.Spec.UID)
}

func (p *Pod) getPod() *v1.Pod {
	s := p.Spec
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name(),
			Namespace: s.Namespace,
			Labels: map[string]string{
				"app": s.App,
			},
		},
		Spec: v1.PodSpec{
			Volumes: nil,
			InitContainers: []v1.Container{{
				Name:  "istio-init",
				Image: "istio/proxyv2",
			}},
			Containers: []v1.Container{{
				Name:  "app",
				Image: "app",
			}, {
				Name:  "istio-proxy",
				Image: "istio/proxyv2",
			}},
		},
		Status: v1.PodStatus{
			Phase:      v1.PodRunning,
			Conditions: nil,
			PodIP:      s.IP,
			PodIPs:     []v1.PodIP{{s.IP}},
		},
	}
}