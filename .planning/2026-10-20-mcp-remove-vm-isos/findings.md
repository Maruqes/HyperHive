# Findings

POST /virsh/removeiso/{vm_name}, no body, plain-text HTTP 200. Removes all CD-ROM devices and CD-ROM boot entries, retaining ISO files. Attempts live detach before defining persistent XML; partial failure possible. User explicitly requested this capability, overriding its narrow prior policy exclusion. Use high risk, existing approval gate and private secure XML/state snapshots. Sources: master/api/virsh.go removeIso; slave/virsh/common.go RemoveIsoFromVM.
