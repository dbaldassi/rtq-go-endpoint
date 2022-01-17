#!/bin/bash

docker run -v $PWD:/root --name receiver --rm --network host -e RECEIVER_PARAMS="-transport $1 -cc $2" -e DESTINATION="/root/out.y4m" rtp-go-endpoint

# out : RTP receiveTime(ms) PayloadType ssrc sequenceNumber timestamp marker?(bool) len(payload)

# Pas de ssim
# CC : SCReAM, SCReAM_INFER (sans rtcp ?), NAIVE_ADAPTION(?), rien (NewReno?)
