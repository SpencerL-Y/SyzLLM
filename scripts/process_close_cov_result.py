import os
import sys
import subprocess


close_addr2funcname_and_filepath = dict()

project_root = "/home/clexma/Desktop/fox3/fuzzing/"
def parse_func2addr_info():
    func2addr_info_path = project_root + "linuxRepo/line2addr/func2addr_info.txt"
    curr_func_name = ""
    curr_file_path = ""
    curr_source = ""
    addr_info_file = open(func2addr_info_path, "r+")
    # 1 for funcname, 3 for source, 2 for filepath and 4 for addresses
    reading_mode = -1
    for line in addr_info_file.readlines():
        if line.find("----- funcname") != - 1:
            reading_mode = 1
            curr_func_name = ""
            continue
        elif line.find("----- filepath") != -1:
            reading_mode = 2
            curr_file_path = ""
            continue
        elif line.find("----- source") != -1:
            reading_mode = 3
            curr_source = ""
        elif line.find("----- addresses") != -1:
            reading_mode = 4
            continue
        else:
            pass
        assert(reading_mode > 0)
        if reading_mode == 1:
            funcname = line.replace("\n", "")
            curr_func_name = funcname
        elif reading_mode == 2:
            curr_file_path = line.replace("\n", "")
        elif reading_mode == 3:
            curr_source = line.replace("\n", "")
        elif reading_mode == 4:
            temp_addr = line.replace("\n", "")
            addr_source_info = curr_file_path + "\n" + curr_func_name + "\n" + curr_source
            close_addr2funcname_and_filepath[temp_addr] = addr_source_info


def collect_close_cov_info():
    close_cov_raw_files = open( project_root + "syzkaller/close_cov_result.txt", "r")
    # 1 for call sequence, 2 for args and 3 for close points
    collecting_mode = -1
    call_sequence = []
    args_sequence = []
    covered_points = []
    temp_args = []
    final_result = []
    is_first_args_batch = True
    for line in close_cov_raw_files.readlines():
        if line.find("----- call sequence") != -1:
            collecting_mode = 1
            continue
        elif line.find("----- arg sequence") != -1:
            collecting_mode = 2
            continue
        elif line.find("----- close points covered") != -1:
            args_sequence.append(temp_args.copy())
            temp_args = []
            collecting_mode = 3
            continue
        elif line.find("=====") != -1:
            if len(covered_points) != 0:
                final_result.append((call_sequence.copy(), args_sequence.copy(), covered_points.copy()))
            call_sequence = []
            args_sequence = []
            covered_points = []
            temp_args = []
            is_first_args_batch = True
            collecting_mode = -1
            continue
        else:
            pass
        assert(collecting_mode > 0)
        if collecting_mode == 1:
            if line.find("----- call") != -1:
                continue
            else:
                call_sequence.append(line.replace("\n", ""))
        elif collecting_mode == 2:
            if line.find("----- args") != -1:
                if is_first_args_batch:
                    is_first_args_batch = False
                    temp_args = []
                else:
                    args_sequence.append(temp_args.copy())
                    temp_args = []
                    
            else:
                temp_args.append(line.replace("\n", ""))
        elif collecting_mode == 3:
            if line.strip().replace("\n", "") != "":
                covered_points.append(line.replace("\n", ""))
        else:
            pass
    close_cov_raw_files.close()
    return final_result

def formulate_program_cov_info_for_llm(close_cov_info):
    program_id = 1
    final_result = ""
    for syscall_prog_tuple in close_cov_info:
        curr_call_sequence = syscall_prog_tuple[0]
        curr_args_sequence = syscall_prog_tuple[1]
        curr_cov_sequence = syscall_prog_tuple[2]

        call_description = "Program " + str(program_id) + " in system call sequence is: \n"
        i = 0
        for call in curr_call_sequence:
            call_description += "(" + str(i) + ") " + call + "\n"
            i += 1
        args_description = "Argument sequence correspond to Program " + str(program_id) + " is: \n"
        i = 0
        for args in curr_args_sequence:
            args_description += "(" + str(i) + ") "
            i += 1
            for arg in args:
                args_description += arg + ", "
            args_description += "\n"
        cov_description = "Program " + str(program_id) + " has coverage: \n"
        for cov in curr_cov_sequence:
            cov_description += close_addr2funcname_and_filepath[cov] + "\n"
        final_result += call_description + args_description + cov_description + "\n"
    return final_result
        
if __name__ == "__main__":
    parse_func2addr_info()
    result = collect_close_cov_info()
    close_cov_source_code = formulate_program_cov_info_for_llm(result)
    close_cov_source_code_file = open(project_root + "ChatAnalyzer/close_cov_prog_source_code.txt", "w+")
    close_cov_source_code_file.write(close_cov_source_code)
    # print(f"Python executable: {sys.executable}")
    # print(f"Environment: {os.environ}")
    os.chdir(project_root + "ChatAnalyzer/")    
    print("close ask")
    close_ask_cmd = "python3 chat_interface.py close_ask"
    result = subprocess.run(close_ask_cmd, shell=True, text=True, capture_output=True, env=os.environ.copy())
    print("close ask Error: " + result.stderr)
    print("close ask Output: " + result.stdout)
    os.truncate(project_root + "ChatAnalyzer/close_cov_prog_source_code.txt", 0)
    os.truncate(project_root + "syzkaller/close_cov_result.txt", 0)
