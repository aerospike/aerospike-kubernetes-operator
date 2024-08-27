#!/bin/sh

# dashboards to download
# DASHBOARDS="<dashboard_id>:<revision> ..."
DASHBOARDS="16119:10 16115:7"

TARGET_DIR="/mnt/data"
mkdir -p "$TARGET_DIR"

for DASHBOARD in $DASHBOARDS; do
  delimiter=':'

  # Count the number of occurrences of the delimiter
  count=$(echo "$DASHBOARD" | awk -F"$delimiter" '{print NF-1}')

  if [ "$count" -eq 1 ]; then
      # Split the string into two parts
      ID=$(echo "$DASHBOARD" | cut -d"$delimiter" -f1)
      REVISION=$(echo "$DASHBOARD" | cut -d"$delimiter" -f2)

      URL=https://grafana.com/api/dashboards/$ID/revisions/$REVISION/download
      FILENAME=$ID-"rev"$REVISION.json
      curl -o "$TARGET_DIR/$FILENAME" "$URL"
  else
      echo "Error: The string must contain exactly one delimiter."
  fi
done

echo "Downloaded files:"
ls -l "$TARGET_DIR"