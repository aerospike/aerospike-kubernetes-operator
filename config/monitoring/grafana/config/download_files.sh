#!/bin/sh

# Check if curl and jq is installed; if not, install curl and jq
if ! command -v curl >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
    echo "curl or jq not found. Installing..."
    apk add --no-cache curl jq
else
    echo "curl and jq are already installed."
fi

# Define the dashboards to download in the format <dashboard_id>:<revision> or <dashboard_id>
DASHBOARDS="16119:13 16115:8 20279:2 23455"

# Directory where the dashboards will be saved
TARGET_DIR="/mnt/data"
mkdir -p "$TARGET_DIR"

DELIMITER=':'

# Loop through each dashboard identifier in DASHBOARDS
for DASHBOARD in $DASHBOARDS; do
  if echo "$DASHBOARD" | grep -q "$DELIMITER"; then
    # If the delimiter ':' exists, split into ID and REVISION
    ID=$(echo "$DASHBOARD" | cut -d"$DELIMITER" -f1)
    REVISION=$(echo "$DASHBOARD" | cut -d"$DELIMITER" -f2)
    FILENAME="$ID-rev$REVISION.json"
    URL="https://grafana.com/api/dashboards/$ID/revisions/$REVISION/download"
    curl -o "$TARGET_DIR/$FILENAME" "$URL"
  else
    # No delimiter, only the ID is provided
    ID="$DASHBOARD"
    FILENAME="$ID.json"
    URL="https://grafana.com/api/dashboards/$ID"
    curl -s "$URL" | jq '.json' > "$TARGET_DIR/$FILENAME"
  fi
done

# List the downloaded files
echo "Downloaded dashboard files:"
ls -l "$TARGET_DIR"