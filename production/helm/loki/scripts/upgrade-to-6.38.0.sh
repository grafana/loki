#!/bin/bash
set -e

# Loki Helm Chart Upgrade Script to 6.38.0+
# This script safely orphans existing StatefulSets before upgrading

RELEASE_NAME=""
NAMESPACE=""
TARGET_VERSION="6.38.0"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    echo "Usage: $0 -r RELEASE_NAME [-n NAMESPACE] [-v TARGET_VERSION]"
    echo ""
    echo "Options:"
    echo "  -r RELEASE_NAME    Name of the Helm release"
    echo "  -n NAMESPACE       Kubernetes namespace (optional, defaults to current context)"
    echo "  -v TARGET_VERSION  Target chart version (default: 6.38.0)"
    echo "  -h                 Show this help message"
    echo ""
    echo "Example:"
    echo "  $0 -r my-loki-release -n loki-namespace -v 6.38.0"
    exit 1
}

log() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# Parse command line arguments
while getopts "r:n:v:h" opt; do
    case $opt in
        r) RELEASE_NAME="$OPTARG" ;;
        n) NAMESPACE="$OPTARG" ;;
        v) TARGET_VERSION="$OPTARG" ;;
        h) usage ;;
        \?) error "Invalid option -$OPTARG" ;;
    esac
done

# Validate required parameters
if [ -z "$RELEASE_NAME" ]; then
    error "Release name is required. Use -r to specify it."
fi

# Validate namespace exists if specified
if [ -n "$NAMESPACE" ] && ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    error "Namespace '$NAMESPACE' does not exist"
fi

# Build namespace flag for kubectl
NAMESPACE_FLAG=""
if [ -n "$NAMESPACE" ]; then
    NAMESPACE_FLAG="-n $NAMESPACE"
fi

# Check if kubectl is available
if ! command -v kubectl &> /dev/null; then
    error "kubectl is not installed or not in PATH"
fi

# Check if helm is available
if ! command -v helm &> /dev/null; then
    error "helm is not installed or not in PATH"
fi

# Verify the release exists
log "Checking if Helm release '$RELEASE_NAME' exists..."
if ! helm status "$RELEASE_NAME" $NAMESPACE_FLAG &> /dev/null; then
    error "Helm release '$RELEASE_NAME' not found"
fi

# List of all possible StatefulSets to check
STATEFULSETS=(
    # Core components (SimpleScalable mode)
    "${RELEASE_NAME}-write"
    "${RELEASE_NAME}-backend"
    
    # Legacy read target
    "${RELEASE_NAME}-read"
    
    # Single binary mode
    "${RELEASE_NAME}"
    
    # Distributed mode components
    "${RELEASE_NAME}-ingester"
    "${RELEASE_NAME}-ingester-zone-a"
    "${RELEASE_NAME}-ingester-zone-b"
    "${RELEASE_NAME}-ingester-zone-c"
    "${RELEASE_NAME}-index-gateway"
    "${RELEASE_NAME}-compactor"
    "${RELEASE_NAME}-ruler"
    "${RELEASE_NAME}-pattern-ingester"
    "${RELEASE_NAME}-bloom-planner"
    "${RELEASE_NAME}-bloom-gateway"
)

# Detect existing StatefulSets using label selector
log "Detecting existing StatefulSets..."
EXISTING_STATEFULSETS=()
RELEASE_LABEL="app.kubernetes.io/instance=$RELEASE_NAME"

for sts in "${STATEFULSETS[@]}"; do
    if kubectl get statefulset "$sts" $NAMESPACE_FLAG -l "$RELEASE_LABEL" &> /dev/null; then
        EXISTING_STATEFULSETS+=("$sts")
        log "Found StatefulSet: $sts"
    fi
done

if [ ${#EXISTING_STATEFULSETS[@]} -eq 0 ]; then
    warn "No StatefulSets found for release '$RELEASE_NAME'"
    log "Proceeding directly to Helm upgrade..."
else
    log "Found ${#EXISTING_STATEFULSETS[@]} StatefulSet(s) that need to be orphaned"
    
    # Show what will be orphaned
    echo ""
    warn "The following StatefulSets will be orphaned (data will be preserved):"
    for sts in "${EXISTING_STATEFULSETS[@]}"; do
        echo "  - $sts"
    done
    echo ""
    
    # Confirm before proceeding
    read -p "Do you want to continue? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log "Operation cancelled by user"
        exit 0
    fi
    
    # Orphan the StatefulSets
    log "Orphaning StatefulSets..."
    for sts in "${EXISTING_STATEFULSETS[@]}"; do
        log "Orphaning StatefulSet: $sts"
        if kubectl delete statefulset "$sts" $NAMESPACE_FLAG --cascade=orphan; then
            success "Successfully orphaned: $sts"
        else
            error "Failed to orphan StatefulSet: $sts"
        fi
    done
    
    success "All StatefulSets have been orphaned successfully"
fi

# Perform the Helm upgrade
log "Performing Helm upgrade to version $TARGET_VERSION..."
echo ""

# Build helm upgrade command
HELM_CMD="helm upgrade $RELEASE_NAME grafana/loki --version $TARGET_VERSION --reuse-values"
if [ -n "$NAMESPACE" ]; then
    HELM_CMD="$HELM_CMD --namespace $NAMESPACE"
fi

log "Running: $HELM_CMD"
echo ""

if $HELM_CMD; then
    success "Helm upgrade completed successfully!"
    echo ""
    log "Verifying StatefulSets are recreated..."
    
    # Wait a moment for StatefulSets to be created
    sleep 5
    
    # Check if StatefulSets were recreated
    RECREATED=0
    for sts in "${EXISTING_STATEFULSETS[@]}"; do
        if kubectl get statefulset "$sts" $NAMESPACE_FLAG &> /dev/null; then
            success "StatefulSet recreated: $sts"
            ((RECREATED++))
        else
            warn "StatefulSet not found after upgrade: $sts"
        fi
    done
    
    echo ""
    success "Upgrade complete! $RECREATED StatefulSet(s) recreated."
    log "Monitor pod status with: kubectl get pods $NAMESPACE_FLAG -l app.kubernetes.io/instance=$RELEASE_NAME"
else
    error "Helm upgrade failed!"
fi

