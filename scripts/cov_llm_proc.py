import os
import time
import sys
import subprocess

if __name__ == "__main__":
    current_file = sys.argv[1]
    print("cov_llm_proc called")
    path = os.path.join(current_file)
    os.system("pwd")
    opened_file = open(path, "r")
    mode = -1
    temp_call_name = ""
    temp_call_args = ""
    call_cov_points = set()
    call_name_list = []
    call_args_str_list = []
    output_file = "./temp.txt"
    with open(output_file, 'a') as f:
        for line in opened_file.readlines():
            if line.startswith("-----"):
                # detecting and changing the mode
                if line.find("call") != -1:
                    mode = 1
                    temp_call_name = ""
                    temp_call_args = ""
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
                    call_cov_points.add(line.replace('\n', '').replace('\r', '').strip())
                    # obtain cover and use addr2line
                else:
                    assert(False)

        command = ["cat"]
        addresses_input = "\n".join(call_cov_points) + "\n"
        
        with open("./temp.txt", "a") as f:
            result = subprocess.run(command, input=addresses_input, text=True, stdout=f)
        os.system("rm " + path)

