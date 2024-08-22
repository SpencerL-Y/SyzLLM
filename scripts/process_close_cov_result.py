import os
import sys




def collect_close_cov_info():
    close_cov_raw_files = open("./close_cov_result.txt")
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
            covered_points.append(line.replace("\n", ""))
        else:
            pass
    return final_result
if __name__ == "__main__":
    result = collect_close_cov_info()
    print(result)