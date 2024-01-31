/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2023 Red Hat, Inc.
 *
 */

package utils

import (
	"context"
	"k8s.io/client-go/kubernetes"
	"kubevirt.io/application-aware-quota/pkg/util"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetLeader(virtClient *kubernetes.Clientset, aaqNS string) string {
	controllerLease, err := virtClient.CoordinationV1().Leases(aaqNS).Get(context.Background(), util.ControllerPodName, v1.GetOptions{})
	if err != nil {
		return ""
	}
	leaderName := controllerLease.Spec.HolderIdentity
	if leaderName == nil {
		return ""
	}
	return *leaderName
}
