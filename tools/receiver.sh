#!/bin/bash

OUT_DIR=out
mkdir -p $OUT_DIR

docker run -v $PWD:/root --cap-add=NET_ADMIN --name receiver --rm  -e RECEIVER_PARAMS="-transport $1 -cc $2"  -e TC_CONFIG="$4 $(cat link_limit.csv | tr '\n' ' ')" -e DESTINATION="/root/$OUT_DIR/out.y4m" -e ROLE="receiver" rtq-go-endpoint-$3

# out : RTP receiveTime(ms) PayloadType ssrc sequenceNumber timestamp marker?(bool) len(payload)

# Pas de ssim
# CC : SCReAM, SCReAM_INFER (sans rtcp ?), NAIVE_ADAPTION(?), rien (NewReno?)
