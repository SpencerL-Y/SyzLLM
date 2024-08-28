# this file is called from syzkaller root path
import os
import time
import sys
import subprocess

if __name__ == "__main__":




    # print("cov_llm_proc called")
    current_file = sys.argv[1]
    close_addr_file_path = "../linuxRepo/line2addr/result_addr_info.txt"
    close_points_set = set()
    # TODO: open the pure addr points in addr2line folder and process the coverage files
    # TODO: add here
    close_addr_file = open(close_addr_file_path, "r")
    # print("collect close addr point")
    for addr_line in  close_addr_file.readlines():
        stripped_close_addr = addr_line.strip().replace("\n", "")
        if "f" in stripped_close_addr:
            close_points_set.add(stripped_close_addr)
    

    
    path = os.path.join(current_file)
    # os.system("pwd")
    opened_file = open(path, "r")
    mode = -1
    temp_call_name = ""
    temp_call_args = ""
    call_cov_points = set()
    total_cov_close_points = set()
    call_name_list = []
    call_args_str_list = []
    contain_close_points = False
    
    
    for line in opened_file.readlines():
        if line.startswith("-----"):
            # detecting and changing the mode
            if line.find("call") != -1:
                mode = 1
                temp_call_name = "----- call\n"
                temp_call_args = "----- args\n"
                call_cov_points = set()
            elif line.find("args") != -1:
                mode = 2
            elif line.find("covers") != -1:
                call_name_list.append(temp_call_name)
                call_args_str_list.append(temp_call_args)
                mode = 3
            else:
                assert(False)
        else:
            assert(mode > 0)
            if mode == 1: 
                # call
                temp_call_name += line
            elif mode == 2:
                # arg
                temp_call_args += line
            elif mode == 3:
                stripped_point = line.replace('\n', '').replace('\r', '').strip()
                if stripped_point in close_points_set:
                    contain_close_points = True
                    call_cov_points.add(stripped_point)
                # obtain cover and use addr2line
            else:
                assert(False)
    command = ["cat"]
    call_sequence = "".join(call_name_list) 
    arg_sequence = "".join(call_args_str_list) 
    addresses_input = "\n".join(call_cov_points) 
    stripped_call_sequence = call_sequence.replace("\n", "").strip()
    stripped_arg_sequence = arg_sequence.replace("\n", "").strip()
    stripped_addresses_input = addresses_input.replace("\n", "").strip()
    if (stripped_addresses_input != "" or stripped_arg_sequence != "" or stripped_call_sequence != "") and contain_close_points: 
        final_write_result = "----- call sequence\n" + call_sequence +\
                             "----- arg sequence\n" + arg_sequence +\
                             "----- close points covered\n" + addresses_input
        final_write_result += "\n=====\n"
    
        with open("./close_cov_result.txt", "a+") as f:
            result = subprocess.run(command, input=final_write_result, text=True, stdout=f)
    os.system("rm " + path)
    if contain_close_points:
        print("XXXXX REACH")

