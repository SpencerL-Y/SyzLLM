import os
import sys


if __name__ == "__main__":
    temp_file = open("./temp.txt")
    call_cov_points = []
    for line in temp_file.readlines():
        striped_line = line.strip().replace("\n", "")
        call_cov_points.append(striped_line)
        pc_map = dict()
    for pc_str in call_cov_points:
        if pc_str in pc_map:
            pc_map[pc_str] += 1
        else:
            pc_map[pc_str] = 1
    for item in pc_map:
        print(item + "," + str(pc_map[item]))