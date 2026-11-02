#!/bin/bash
set -euo pipefail

KK="kubevirtci/cluster-up/kubectl.sh"
NS="default"
TOTAL_VMIS=5
MAX_MIGRATING=2

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
echo "Will create $TOTAL_VMIS VMIs, start them, then migrate all"
echo "Quota allows $MAX_MIGRATING migrating at a time"

NODES=$($KK get nodes --no-headers 2>/dev/null | grep -c "Ready" || true)
if [[ "$NODES" -lt 2 ]]; then
    echo "ERROR: Need at least 2 nodes for migration testing, found $NODES"
    exit 1
fi
echo "Cluster has $NODES nodes"

TEST_LABEL="migrate-test-$(date +%s)"

cleanup() {
    echo ""
    echo "=== Cleanup ==="
    $KK delete virtualmachineinstancemigration -l test-run="$TEST_LABEL" -n $NS --ignore-not-found 2>/dev/null || true
    $KK delete vmi -l test-run="$TEST_LABEL" -n $NS --ignore-not-found 2>/dev/null || true
    $KK delete arq migrating-vmi-quota -n $NS --ignore-not-found 2>/dev/null || true
    $KK delete aaqjqc -n $NS --ignore-not-found 2>/dev/null || true
    echo "Done."
}
trap cleanup EXIT

echo ""
echo "=== Step 1: Create VmiMigrating scoped ARQ (max $MAX_MIGRATING migrating VMIs) ==="
$KK apply -f - <<EOF
apiVersion: aaq.kubevirt.io/v1alpha1
kind: ApplicationAwareResourceQuota
metadata:
  name: migrating-vmi-quota
  namespace: $NS
spec:
  hard:
    $CPU_RESOURCE: "$MAX_MIGRATING"
    $MEM_RESOURCE: "$((MAX_MIGRATING * 512))Mi"
  scopes:
    - VmiMigrating
EOF

sleep 5
echo "Quota created."

echo ""
echo "=== Step 2: Create $TOTAL_VMIS VMIs and wait for them to be Running ==="
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
echo "Waiting for all VMIs to reach Running..."
for attempt in $(seq 1 60); do
    running=$($KK get vmi -l test-run="$TEST_LABEL" -n $NS -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null | grep -c "Running" || true)
    echo "  $running/$TOTAL_VMIS Running"
    if [[ "$running" -eq "$TOTAL_VMIS" ]]; then
        echo "All VMIs are Running."
        break
    fi
    sleep 5
done

echo ""
echo "Quota before migration:"
$KK get arq migrating-vmi-quota -n $NS -o jsonpath='{.status.used}' 2>/dev/null; echo ""

echo ""
echo "=== Step 3: Trigger migration for all $TOTAL_VMIS VMIs ==="
for i in $(seq 1 $TOTAL_VMIS); do
    $KK apply -f - <<EOF 2>/dev/null
apiVersion: kubevirt.io/v1
kind: VirtualMachineInstanceMigration
metadata:
  name: migrate-vmi-$i
  namespace: $NS
  labels:
    test-run: "$TEST_LABEL"
spec:
  vmiName: test-vmi-$i
EOF
    echo "  Triggered migration for test-vmi-$i"
done

echo ""
echo "=== Step 4: Watching (expect at most $MAX_MIGRATING migrating at a time) ==="
printf "%-10s %-12s %-12s %-12s %s\n" "TIME" "MIGRATING" "SUCCEEDED" "GATED" "QUOTA USED"
printf "%-10s %-12s %-12s %-12s %s\n" "----" "---------" "---------" "-----" "----------"

max_migrating_seen=0
for i in $(seq 1 60); do
    vmim_phases=$($KK get virtualmachineinstancemigration -l test-run="$TEST_LABEL" -n $NS -o jsonpath='{range .items[*]}{.status.phase}{"\n"}{end}' 2>/dev/null)
    active=$(echo "$vmim_phases" | grep -cE "Scheduling|Scheduled|PreparingTarget|TargetReady|Running" || true)
    succeeded=$(echo "$vmim_phases" | grep -c "Succeeded" || true)

    gated=$($KK get pods -n $NS -l test-run="$TEST_LABEL" -o jsonpath='{range .items[*]}{.spec.schedulingGates}{"\n"}{end}' 2>/dev/null | grep -c "ApplicationAwareQuotaGate" || true)

    if [[ $active -gt $max_migrating_seen ]]; then
        max_migrating_seen=$active
    fi

    used=$($KK get arq migrating-vmi-quota -n $NS -o jsonpath='{.status.used}' 2>/dev/null || echo "?")

    printf "%-10s %-12s %-12s %-12s %s\n" "$(date +%H:%M:%S)" "$active" "$succeeded" "$gated" "$used"

    if [[ "$succeeded" -eq "$TOTAL_VMIS" ]]; then
        echo ""
        echo "All $TOTAL_VMIS migrations completed."
        break
    fi
    sleep 5
done

echo ""
echo "=== Results ==="
echo "Max actively migrating VMIs seen simultaneously: $max_migrating_seen"
if [[ $max_migrating_seen -le $MAX_MIGRATING ]]; then
    echo "PASS: Never exceeded $MAX_MIGRATING migrating VMIs at a time"
else
    echo "FAIL: Saw $max_migrating_seen migrating VMIs (limit was $MAX_MIGRATING)"
fi

echo ""
echo "Final quota:"
$KK get arq migrating-vmi-quota -n $NS -o jsonpath='{.status.used}' 2>/dev/null; echo ""
