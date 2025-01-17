package controller

import (
	kubeovnv1 "github.com/alauda/kube-ovn/pkg/apis/kubeovn/v1"

	"k8s.io/klog"
)

func (c *Controller) enqueueAddOrDelIP(obj interface{}) {
	if !c.isLeader() {
		return
	}
	ipObj := obj.(*kubeovnv1.IP)
	klog.V(3).Infof("enqueue update status subnet %s", ipObj.Spec.Subnet)
	c.updateSubnetStatusQueue.AddRateLimited(ipObj.Spec.Subnet)
}
