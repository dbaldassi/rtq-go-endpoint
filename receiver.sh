#!/bin/bash

docker run -v $PWD:/root --name receiver --rm  -e RECEIVER_PARAMS="-transport $1 -cc $2" -e DESTINATION="/root/out.y4m" -e ROLE="receiver" rtq-go-endpoint-$3

# out : RTP receiveTime(ms) PayloadType ssrc sequenceNumber timestamp marker?(bool) len(payload)

# Pas de ssim
# CC : SCReAM, SCReAM_INFER (sans rtcp ?), NAIVE_ADAPTION(?), rien (NewReno?)
