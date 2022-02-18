#!/bin/python3

import matplotlib.pyplot as plt
import csv
import sys

time = []
target = []

# -- Scream Stats
transmitted = []
loss = []
isInFastStart = []
rtpQueueDelay = []
sRTT = []
cwnd = []
bytesInFlight = []

# -- QUIC Stats
latestRTT = []
packetLoss = []
packetDropped = []
slowStart = []
recovery = []
avoidance = []
appLimited = []

# -- Global
timestamp_bandwidth = []
send_bandwidth = []
receive_bandwidth = []
total_bandwidth = []

# params
limit1 = 1000 # bps
limit2 = 500 # bps
time1 = 60 # sec
time2 = 90 # sec
videotime = 120 # sec

if len(sys.argv) >= 2:
    fig_name = sys.argv[1]
else:
    fig_name = "result"

# Parse csv file

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
            sRTT.append(float(row[0]))
            cwnd.append(int(row[1]))
            bytesInFlight.append(int(row[2]))
            transmitted.append(int(row[3]))
            isInFastStart.append(int(row[4]))
            rtpQueueDelay.append(float(row[5]))
            loss.append(int(row[6]))

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
        timestamp_bandwidth.append(float(row[0]))
        receive_bandwidth.append(float(row[1]) * 8. / 1000.)

with open('send_bandwidth.csv', 'r') as csvfile:
    lines = csv.reader(csvfile, delimiter=',')
    for row in lines:
        send_bandwidth.append(float(row[1]) * 8. / 1000.)


total_bandwidth = [send_bandwidth[i] + receive_bandwidth[i] for i in range(len(send_bandwidth))]
    
# Plot results

fig = plt.figure(figsize=(20,11.25))
plt.title(fig_name, fontsize = 20)
# fig.title(fig_name, fontsize = 20)

plt.subplot(321)
plt.plot(time, target, color = 'r', label = "target")
plt.plot([0,time1,time1,time2,time2,videotime],
         [limit1,limit1,limit2,limit2,limit1,limit1],
         label = "link", color = 'k')

plt.plot(timestamp_bandwidth, send_bandwidth, color = 'y', label = "send_bandwidth")
plt.plot(timestamp_bandwidth, receive_bandwidth, color = 'c', label = "receive_bandwidth")
plt.plot(timestamp_bandwidth, total_bandwidth, color = 'g', label = "total_bandwidth")

plt.legend()
plt.grid()

if(len(transmitted) > 0):
    plt.plot(time, transmitted, color = 'b', label = "transmitted")
    plt.legend()
    
    plt.subplot(322)
    plt.plot(time, rtpQueueDelay, color = 'r', label = "rtp queue delay")
    plt.plot(time, sRTT, color = 'b', label = "sRTT")
    plt.legend()
    plt.grid()

    plt.subplot(323)
    plt.plot(time, loss, label = "loss rate")
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
