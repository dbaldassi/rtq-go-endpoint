#!/bin/bash

NIC=lo
QDISC=tbf
BURST=50kb

if [ $# -eq 0 ]
then
    sudo tc qdisc delete dev $NIC root
else
    sudo tc qdisc add dev $NIC root $QDISC rate $1mbit burst $BURST latency $5ms

    sleep $3

    sudo tc qdisc replace dev $NIC root $QDISC rate $2mbit burst $BURST latency $5ms

    sleep $4

    sudo tc qdisc replace dev $NIC root $QDISC rate $1mbit burst $BURST latency $5ms
fi
