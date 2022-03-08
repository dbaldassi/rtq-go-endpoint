#!/bin/python3

import matplotlib.pyplot as plt
import csv
import sys

time = []
target = []

# -- Scream Stats
transmitted = []
loss = []
cleared = []
isInFastStart = []
rtpQueueDelay = []
sRTT = []
cwnd = []
bytesInFlight = []
queueDelay = []
queueDelayMax = []

# -- QUIC Stats
latestRTT = []
packetLoss = []
packetDropped = []
slowStart = []
recovery = []
avoidance = []
appLimited = []

# -- Global
timestamp_r_bandwidth = []
timestamp_s_bandwidth = []
send_bandwidth = []
receive_bandwidth = []
total_bandwidth = []

# params
# limit1 = 1000 # bps
# limit2 = 500 # bps
# time1 = 60 # sec
# time2 = 90 # sec
# videotime = 120 # sec
link_time  = []
link_limit = []
show_limit = True

if len(sys.argv) >= 2:
    fig_name = sys.argv[1]
else:
    fig_name = "result"

# Parse csv file

## parse link limit
with open('link_limit.csv', 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')

    for row in lines:
        if(row[0] == 'NONE'):
            show_limit = False
            continue
        link_time.append(int(row[0]))
        link_time.append(int(row[1]))

        limit = float(row[2]) * 1000
        link_limit.append(limit)
        link_limit.append(limit)

## parse time and target bitrate
with open('bitrate.csv', 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')

    for row in lines:
        # -- Parse timestamp and target
        time.append(float(row[0]))
        target.append(int(row[1]))
        
    # Convert timestamp in seconds (120 seconds long)
    t0 = time[0]
    time = [float(t - t0)/float(1000) for t in time]

## parse scream stats
with open('scream.csv', 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')

    for row in lines:
        if(row[0] == ''):
            continue
        else:
            queueDelay.append(float(row[0]))
            queueDelayMax.append(float(row[1]))
            sRTT.append(float(row[2]))
            cwnd.append(int(row[3]))
            bytesInFlight.append(int(row[4]))
            transmitted.append(int(row[5]))
            isInFastStart.append(int(row[6]))
            rtpQueueDelay.append(float(row[7]))
            cleared.append(int(row[8]))
            loss.append(int(row[9]))

## parse QUIC stats
# with open('qstats.csv', 'r') as csvfile:
#     lines = csv.reader(csvfile, delimiter=',')

#     for row in lines:
#         latestRTT.append(float(row[0]))
#         packetLoss.append(int(row[1]))
#         packetDropped.append(int(row[2]))
#         slowStart.append(0 if(row[3]=='false') else 1)
#         recovery.append(0 if(row[4]=='false') else 1)
#         avoidance.append(0 if(row[5]=='false') else 1)
#         appLimited.append(0 if(row[6]=='false') else 1)

## parse tcpdump output
with open('receive_bandwidth.csv', 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')
    for row in lines:
        timestamp_r_bandwidth.append(float(row[0]))
        receive_bandwidth.append(float(row[1]) * 8. / 1000.)

with open('send_bandwidth.csv', 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')
    for row in lines:
        timestamp_s_bandwidth.append(float(row[0]))
        send_bandwidth.append(float(row[1]) * 8. / 1000.)


total_bandwidth = [send_bandwidth[i] + receive_bandwidth[i] for i in range(len(send_bandwidth))]
    
# Plot results

fig = plt.figure(figsize=(20,11.25))
plt.title(fig_name, fontsize = 20)
# fig.title(fig_name, fontsize = 20)

plt.subplot(321)

if(show_limit):
    plt.plot(link_time, link_limit, label = "link", color = 'k')

plt.plot(time, target, color = 'r', label = "target")

plt.plot(timestamp_s_bandwidth, send_bandwidth, color = 'g', label = "send_bandwidth")
plt.plot(timestamp_r_bandwidth, receive_bandwidth, color = 'c', label = "receive_bandwidth")
# plt.plot(timestamp_bandwidth, total_bandwidth, color = 'g', label = "total_bandwidth")

plt.legend()
plt.grid()

if(len(transmitted) > 0):
    plt.plot(time, transmitted, color = 'b', label = "transmitted")
    plt.legend()
    
    plt.subplot(322)
    plt.plot(time, rtpQueueDelay, color = 'y', label = "rtp queue delay")
    plt.plot(time, queueDelay, color = 'g', label = "queue delay")
    plt.plot(time, queueDelayMax, color = 'r', label = "queue delay max")
    plt.plot(time, sRTT, color = 'b', label = "sRTT")
    plt.legend()
    plt.grid()

    plt.subplot(323)
    plt.plot(time, cleared, label = "cleared")
    plt.plot(time, loss, label = "loss")
    plt.legend()
    plt.grid()

    plt.subplot(324)
    plt.plot(time, cwnd, color = 'r', label = "cwnd")
    plt.plot(time, bytesInFlight, color = 'b', label = "BytesInFlight")
    plt.plot(time, [i*max(cwnd) for i in isInFastStart], color = 'y', label = "isInFastStart")
    plt.legend()
    plt.grid()

# if(len(latestRTT) > 0):
#     plt.subplot(325)
#     plt.plot(time, latestRTT, color = 'b', label = "latestRTT")
#     plt.plot(time, packetLoss, color = 'r', label = "packet loss")
#     plt.plot(time, packetDropped, color = 'b', label = "packet dropped")
#     plt.plot(time, [i*max(latestRTT) for i in slowStart], color = 'y', label = "slowstart")
#     plt.plot(time, [i*max(latestRTT) for i in recovery], color = 'g', label = "recovery")
#     plt.plot(time, [i*max(latestRTT) for i in avoidance], color = 'k', label = "avoidance")
#     plt.plot(time, [i*max(latestRTT) for i in appLimited], color = 'c', label = "appLimited")
#     plt.legend()
#     plt.grid()

if len(sys.argv) == 3 and sys.argv[2] == 'save':
    plt.savefig(fig_name + '.png')
else:
    plt.show()
