/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"k8s.io/klog/v2"

	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
	"sigs.k8s.io/etcd-manager/pkg/volumes"
)

// AWSVolumes also allows us to discover our peer nodes
var _ discovery.Interface = &AWSVolumes{}

func (a *AWSVolumes) Poll() (map[string]discovery.Node, error) {
	ctx := context.TODO()
	nodes := make(map[string]discovery.Node)

	allVolumes, err := a.findVolumes(ctx, false)
	if err != nil {
		return nil, err
	}

	instanceToVolumeMap := make(map[string]*volumes.Volume)
	for _, v := range allVolumes {
		if v.AttachedTo != "" {
			instanceToVolumeMap[v.AttachedTo] = v
		}
	}

	if len(instanceToVolumeMap) != 0 {
		request := &ec2.DescribeInstancesInput{}
		for id := range instanceToVolumeMap {
			request.InstanceIds = append(request.InstanceIds, id)
		}

		response, err := a.ec2.DescribeInstances(ctx, request)
		if err != nil {
			return nil, fmt.Errorf("error from AWS DescribeInstances: %v", err)
		}

		for _, reservation := range response.Reservations {
			for _, instance := range reservation.Instances {
				volume := instanceToVolumeMap[aws.ToString(instance.InstanceId)]
				if volume == nil {
					// unexpected ... we constructed the request from the map!
					klog.Errorf("instance not found: %q", aws.ToString(instance.InstanceId))
					continue
				}

				// We use the etcd node ID as the persistent identifier, because the data determines who we are
				node := discovery.Node{
					ID: volume.EtcdName,
				}

				if instance.Ipv6Address != nil {
					ip := *instance.Ipv6Address
					node.Endpoints = append(node.Endpoints, discovery.NodeEndpoint{IP: ip})
				} else {
					if aws.ToString(instance.PrivateIpAddress) != "" {
						ip := aws.ToString(instance.PrivateIpAddress)
						node.Endpoints = append(node.Endpoints, discovery.NodeEndpoint{IP: ip})
					}
					for _, ni := range instance.NetworkInterfaces {
						if aws.ToString(ni.PrivateIpAddress) != "" {
							ip := aws.ToString(ni.PrivateIpAddress)
							node.Endpoints = append(node.Endpoints, discovery.NodeEndpoint{IP: ip})
						}
					}
				}
				nodes[node.ID] = node
			}
		}
	}

	return nodes, nil
}
