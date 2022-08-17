#!/bin/bash

root_OutputDir="./scrapperlogs"
today=`date +%m-%d-%Y`
output_Context_Directory="$(kubectl config current-context)_$today"

output_Directory_Pod="${root_OutputDir}/${output_Context_Directory}/Pod"
describe_Directory_Pod="${output_Directory_Pod}/describe"
logs_Directory_Pod="${output_Directory_Pod}/logs"

eventlogs_Directory="${root_OutputDir}/${output_Context_Directory}/eventlogs"

output_Directory_STS="${root_OutputDir}/${output_Context_Directory}/STS"
describe_Directory_STS="${output_Directory_STS}/describe"

output_Directory_Aero="${root_OutputDir}/${output_Context_Directory}/AeroCluster"
describe_Directory_Aero="${output_Directory_Aero}/describe"

output_Directory_PVC="${root_OutputDir}/${output_Context_Directory}/PVC"
describe_Directory_PVC="${output_Directory_PVC}/describe"

extension="log"
echo "Using output dir $output_Context_Directory"
mkdir -p "$root_OutputDir"
mkdir -p "$output_Directory_Pod"
mkdir -p "$describe_Directory_Pod"
mkdir -p "$logs_Directory_Pod"
mkdir -p "$eventlogs_Directory"
mkdir -p "$output_Directory_STS"
mkdir -p "$describe_Directory_STS"
mkdir -p "$output_Directory_Aero"
mkdir -p "$describe_Directory_Aero"
mkdir -p "$output_Directory_PVC"
mkdir -p "$describe_Directory_PVC"

count=1
podobject=$(kubectl get pods --all-namespaces -o go-template --template '{{range .items}}{{.metadata.name}}{{" "}}{{.metadata.namespace}}{{"\n"}}{{end}}')
for object in $podobject
do
    if [ $((count%2)) -eq 1 ];
    then
        podname=$object
        count=$count+1
        continue
    else
        namespace=$object
    fi
    count=$count+1
    echo $namespace "contains" $podname
    filename="${describe_Directory_Pod}/${namespace}.${podname}.describe"
    kubectl describe pod -n "$namespace" "$podname" > "$filename"
    for container in $(kubectl get po -n "$namespace" "$podname" -o jsonpath="{.spec.containers[*].name}")
    do
        filename_Prefix="$logs_Directory_Pod"/"$namespace"."$podname"."$container"
        filename=$filename_Prefix".current."$extension
        kubectl logs -n "$namespace" "$podname" "$container" > "$filename"
        filename=$filename_Prefix".previous."$extension
        kubectl logs -p -n "$namespace" "$podname" "$container" > "$filename" 2>/dev/null
    done

    for initcontainer in $(kubectl get po -n "$namespace" "$podname" -o jsonpath="{.spec.initContainers[*].name}")
    do
        filename_Prefix="$logs_Directory_Pod"/"$namespace"."$podname"."$initcontainer"
        filename=$filename_Prefix".current."$extension
        kubectl logs -n "$namespace" "$podname" "$initcontainer" > "$filename"
        filename=$filename_Prefix".previous."$extension
        kubectl logs -p -n "$namespace" "$podname" "$initcontainer" > "$filename" 2>/dev/null
    done
done

count=1
stsobject=$(kubectl get sts --all-namespaces -o go-template --template '{{range .items}}{{.metadata.name}}{{" "}}{{.metadata.namespace}}{{"\n"}}{{end}}')
for object in $stsobject
do
    if [ $((count%2)) -eq 1 ];
    then
        stsname=$object
        count=$count+1
        continue
    else
        namespace=$object
    fi
    count=$count+1
    echo $namespace "contains" $stsname
    filename="${describe_Directory_STS}/${namespace}.${stsname}.describe"
    kubectl describe sts -n "$namespace" "$stsname" > "$filename"

done

count=1
aeroclusterobject=$(kubectl get aerospikecluster --all-namespaces -o go-template --template '{{range .items}}{{.metadata.name}}{{" "}}{{.metadata.namespace}}{{"\n"}}{{end}}')
for object in $aeroclusterobject
do
    if [ $((count%2)) -eq 1 ];
    then
        aeroclustername=$object
        count=$count+1
        continue
    else
        namespace=$object
    fi
    count=$count+1
    echo $namespace "contains" $aeroclustername
    filename="${describe_Directory_Aero}/${namespace}.${aeroclustername}.describe"
    kubectl describe sts -n "$namespace" "$aeroclustername" > "$filename"

done

count=1
pvcobject=$(kubectl get pvc --all-namespaces -o go-template --template '{{range .items}}{{.metadata.name}}{{" "}}{{.metadata.namespace}}{{"\n"}}{{end}}')
for object in $pvcobject
do
    if [ $((count%2)) -eq 1 ];
    then
              pvcname=$object
              count=$count+1
              continue
          else
              namespace=$object
          fi
          count=$count+1
          echo $namespace "contains" $pvcname
          filename="${describe_Directory_PVC}/${namespace}.${pvcname}.describe"
          kubectl describe pvc -n "$namespace" "$pvcname" > "$filename"

      done

filename="$eventlogs_Directory/events.log"
kubectl get events -A > "$filename"