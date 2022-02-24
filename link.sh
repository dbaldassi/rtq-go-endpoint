#!/bin/bash

NIC=eth0
BURST=20kb
LATENCY=400

 echo "TC parameters : $# $@"

DELAY=$1
shift
CONFIG=$@

if [ ! "$CONFIG" == "NONE" ]
then
    
    for i in ${CONFIG[@]}
    do
	BEGIN=$(echo $i | cut -d',' -f1)
	END=$(echo $i | cut -d',' -f2)
	LIMIT=$(echo $i | cut -d',' -f3)

	if [ "$BEGIN" == "0" ]
	then
	    echo "link.sh : add netem qdisc with delay $DELAY"
	    tc qdisc add dev $NIC root handle 1: netem delay ${DELAY}ms
	    echo "link.sh : add tbf qdisc with limit $LIMIT"
	    tc qdisc add dev $NIC parent 1: handle 2: tbf rate ${LIMIT}mbit burst $BURST latency ${LATENCY}ms
	else
	    echo "link.sh : change tbf qdisc with limit $LIMIT"
	    tc qdisc change dev $NIC parent 1: handle 2: tbf rate ${LIMIT}mbit burst $BURST latency ${LATENCY}ms
	fi

	let SLEEP=$END-$BEGIN
	echo "link.sh : sleep for $SLEEP seconds"
	sleep $SLEEP
    done

    tc qdisc delete dev $NIC root handle 1:
fi
