#!/bin/bash
set -euo pipefail

KK="kubevirtci/cluster-up/kubectl.sh"
NS="default"
TOTAL_VMIS=10
MAX_STARTING=2

CALC_CONFIG=$($KK get aaq aaq -o jsonpath='{.spec.configuration.vmiCalculatorConfiguration.configName}' 2>/dev/null)
echo "VMI calculator config: $CALC_CONFIG"

case "$CALC_CONFIG" in
  DedicatedVirtualResources)
    CPU_RESOURCE="cpu/vmi"
    MEM_RESOURCE="memory/vmi"
    ;;
  *)
    CPU_RESOURCE="requests.cpu"
    MEM_RESOURCE="requests.memory"
    ;;
esac

echo "Using resources: $CPU_RESOURCE, $MEM_RESOURCE"
echo "Will create $TOTAL_VMIS VMIs, quota allows $MAX_STARTING starting at a time"

TEST_LABEL="scope-test-$(date +%s)"

cleanup() {
    echo ""
    echo "=== Cleanup ==="
    $KK delete vmi -l test-run="$TEST_LABEL" -n $NS --ignore-not-found 2>/dev/null || true
    $KK delete arq starting-vmi-quota -n $NS --ignore-not-found 2>/dev/null || true
    $KK delete aaqjqc -n $NS --ignore-not-found 2>/dev/null || true
    echo "Done."
}
trap cleanup EXIT

echo ""
echo "=== Step 1: Create VmiStarting scoped ARQ (max $MAX_STARTING starting VMIs) ==="
# Each VMI requests 1 CPU, so limit to MAX_STARTING CPUs
$KK apply -f - <<EOF
apiVersion: aaq.kubevirt.io/v1alpha1
kind: ApplicationAwareResourceQuota
metadata:
  name: starting-vmi-quota
  namespace: $NS
spec:
  hard:
    $CPU_RESOURCE: "$MAX_STARTING"
    $MEM_RESOURCE: "$((MAX_STARTING * 512))Mi"
  scopes:
    - VmiStarting
EOF

sleep 5
echo "Quota created. Hard limits: $CPU_RESOURCE=$MAX_STARTING, $MEM_RESOURCE=$((MAX_STARTING * 512))Mi"

echo ""
echo "=== Step 2: Create $TOTAL_VMIS VMIs ==="
for i in $(seq 1 $TOTAL_VMIS); do
    $KK apply -f - <<EOF 2>/dev/null
apiVersion: kubevirt.io/v1
kind: VirtualMachineInstance
metadata:
  name: test-vmi-$i
  namespace: $NS
  labels:
    test-run: "$TEST_LABEL"
spec:
  domain:
    resources:
      requests:
        memory: 512Mi
        cpu: "1"
    devices:
      disks:
        - name: containerdisk
          disk:
            bus: virtio
      interfaces:
        - name: default
          masquerade: {}
  networks:
    - name: default
      pod: {}
  volumes:
    - name: containerdisk
      containerDisk:
        image: quay.io/kubevirt/cirros-container-disk-demo:latest
EOF
    echo "  Created test-vmi-$i"
done

echo ""
echo "=== Step 3: Watching (expect at most $MAX_STARTING non-Running VMIs at a time) ==="
printf "%-10s %-6s %-8s %-8s %-10s %s\n" "TIME" "PODS" "RUNNING" "GATED" "UNGATED-ST" "QUOTA USED"
printf "%-10s %-6s %-8s %-8s %-10s %s\n" "----" "----" "-------" "-----" "----------" "----------"

max_ungated_starting=0
for i in $(seq 1 60); do
    total_pods=$($KK get pods -n $NS -l test-run="$TEST_LABEL" --no-headers 2>/dev/null | wc -l || echo 0)
    running=$($KK get vmi -l test-run="$TEST_LABEL" -n $NS -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null | grep -c "Running" || true)

    gated=$($KK get pods -n $NS -l test-run="$TEST_LABEL" -o jsonpath='{range .items[*]}{.spec.schedulingGates}{"\n"}{end}' 2>/dev/null | grep -c "ApplicationAwareQuotaGate" || true)

    ungated_starting=$((total_pods - gated - running))
    if [[ $ungated_starting -lt 0 ]]; then ungated_starting=0; fi
    if [[ $ungated_starting -gt $max_ungated_starting ]]; then
        max_ungated_starting=$ungated_starting
    fi

    used=$($KK get arq starting-vmi-quota -n $NS -o jsonpath='{.status.used}' 2>/dev/null || echo "?")

    printf "%-10s %-6s %-8s %-8s %-10s %s\n" "$(date +%H:%M:%S)" "$total_pods" "$running" "$gated" "$ungated_starting" "$used"

    if [[ "$running" -eq "$TOTAL_VMIS" ]]; then
        echo ""
        echo "All $TOTAL_VMIS VMIs are Running."
        break
    fi
    sleep 5
done

echo ""
echo "=== Results ==="
echo "Max ungated starting VMIs seen simultaneously: $max_ungated_starting"
if [[ $max_ungated_starting -le $MAX_STARTING ]]; then
    echo "PASS: Never exceeded $MAX_STARTING ungated starting VMIs at a time"
else
    echo "FAIL: Saw $max_ungated_starting ungated starting VMIs (limit was $MAX_STARTING)"
fi
