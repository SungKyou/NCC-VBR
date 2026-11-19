#include		"bs_common.h"
#define			MAX_EVENT_MAX				30
#define			MAX_2ND_DIR					200
#define			VERSION						"2.0672"

// 2.062		BS_Tsyslog2 함수 추가 : 디렉토리 생성, 파일 복사시 용량 포함 리스트 로그 저장  추가 (for 장학재단)
// 2.063		BS_Tsyslog2 함수 log file 교체 로직 변경 및 성능 향상을 목적으로 함수 disable
// 2.065		백업 제외 로직 추가 (for 장학재단 요청 사항 2020.12.8)
// 2.067		백업 제외 대상 directory 20-> 200개로 확장 요청 받음 (for 장학재단 요청 사항 2023.2.9)
// 2.0671		vbr_cifs 작업별로 private queue 사용하는 것으로 변경
// 2.0672		백업 에러가 발생하는 경우 별도 log로 저장하고 전체 error count 누적 추가

typedef struct
{
	char 			status;
	char			curr_file[MAX_FILE_LEN];
	// 해당 thread에서 백업되는 정보 누적 관리, 백업 종료시 thread 정보를 합산하여 관리
	unsigned long	file_cnt;
	unsigned long	dir_cnt;
	unsigned long	change_file_cnt;
	unsigned long	change_dir_cnt;
	// SK 현재 백업중인 sub dir 정보 별도 관리, 해당 sub dir 종료되면 clear
	unsigned long	thread_file_cnt;
	unsigned long	thread_dir_cnt;
	unsigned long	thread_change_file_cnt;
	unsigned long	thread_change_dir_cnt;
	char			last_time[20];					// 최종 업데이트 일자 / 시간
} CIFS_THREAD;

typedef struct {
	long 	msg_type;
	char 	msg_text[MAX_QUEUE_LEN];
} Msg;

typedef struct
{
	char			status;							// '0' : 초기화, '1' : 미송신, '9' : 송신 완료
	char			event_type;						// 'C'Critical, 'W'Warning, 'N'Notice
	char			event_time[20];					// event 발생 시간
	char			event_code[10];					// event code (TBD)
	char			event_msg[200];					// event text message
} EVENT_MSG;

typedef struct
{
	int				status;							// 현재 처리 상태 (0: 미사용/처리완료, 1: 미처리, '3': QUEUE 송신, '5': 결과 송신 완료, '9': 내부 오류)
	int				fd;								// 송신 서버의 fd 저장 (응답 처리용)
	int				msg_len;						// 전문 전체 길이
	long			queue_seq;						// Queue 저장 unique sequence
	char 			msg_type;						// message type, Backup의 경우 'B'로 고정
	char			msg_seq[17];					// message sequence  YYYYMMDDHHMISS + 1/1000sec
	char			task_type;						// 송수신 전문 task 구분
	char			timeout[3];						// Sync 처리 timeout value (sec, 0 : 무한 대기)
	char			priority;						// 처리 우선 순위 (0 : 즉시, 5 : 일반, 9 : 다른 task 우선 처리)
	char			msg_text[MAX_QUEUE_LEN];		// 원격 송신 자료, 처리 결과 자료
} BACKUP_INFO;

CIFS_THREAD		cifs_thread[MAX_CIFS_THREAD+1];
pthread_t		cifs_thread_array[MAX_CIFS_THREAD+1];
key_t			cifs_msg_key		=	30001;		// (CIFS)
int				cifs_queue_id;
char			src_directory[MAX_FILE_LEN], tgt_directory[MAX_FILE_LEN];
char			src_directory_file[MAX_FILE_LEN], tgt_directory_file[MAX_FILE_LEN];
int				event_register(char, char *, char *);
EVENT_MSG		event_msg[2];
int  			portal_info;					// portal 정보 setup 확인 (TRUE/FALSE)
char			portal_ip[100];					// portal ip address
int				portal_port;					// portal listen port
long			backup_time;
time_t			backup_start_time;
struct	tm*		lt_today;

// 2.05
// 1단계 하부 디렉토리 정책 백업이 아닌 2단계까지 내려가는 하부 디렉토리 백업 대상 폴더 (용량이 많거나, 파일 개수가 많아 병렬처리가 추가로 필요한 디렉토리만 (node_info.cfg에 정의)
char 			next_dir[MAX_2ND_DIR][512];
char 			exclusion_dir[MAX_2ND_DIR][512];
int				next_dir_mode;			// 2단계 백업 모드 TRUE/FALSE
int				exclusion_dir_mode;		// 특정 폴더 백업 제외 모드 TRUE/FALSE

void *cifs_processing(void *);
int	setup_next_dir(char *);
int	setup_exclusion_dir(char *);
int	env_initialize();
int portal_initialize();
int	cifs_root(char *, char *);
int	thread_initialize();
int	file_check(char *, char *);
int	file_copy(char *, char *);
int	cifs_sub(int , char *, char *);
int	send_portal(int );
int	prepare_dis_target_dir(char *, char *);
int	directory_is_empty(char *);

int	main(int argc, char	*argv[])
{
	char 		buffer[1024];
	int			i, finish_cnt, path_len;
	// int			thr_id;
	unsigned long	total_file 		= 0L;
	unsigned long	total_dir		= 0L;
	unsigned long	change_file 	= 0L;
	unsigned long	change_dir		= 0L;
	char		history_file[256];
	char		backup_string[1024];
	FILE		*fp;
	char		*point;

	if ((argc == 2) && ((strcmp(argv[1], "-version") == 0) || (strcmp(argv[1], "-ver") == 0)))
	{
		printf("NCC-VBR CIFS Manager version = %s\n", VERSION);
		exit(0);
	}

	BS_initialize();
	// log file max size setup
	// 2017. 5. 22 장학재단 초기 log file 설정 변경 
	if (BS_GetProfileString("VBR Manager", "MAX Log size", buffer, sizeof(buffer)) == FALSE)
    {
    	max_log_file_size	=	50000000L;
    	BS_Tsyslog("Config file[%s] [%s] parameter not found.(%ld default setting)..\n", CONFIG_FILE, "MAX Log size", max_log_file_size);
    }
    else
    {
    	max_log_file_size = atol(buffer);
    	BS_Tsyslog("CIFS Manager : Log File Max size is [%ld]\n", max_log_file_size);
    }

	if (argc != 4)
	{
		BS_Tsyslog("VBR CIFS backup internal error.\n", argc);  		
		BS_Tsyslog("Usage : %s source-dir target-dir seq-no\n", argv[0]);
		printf("Usage : %s source-dir target-dir seq-no\n", argv[0]);
		exit(-1);
	}
    
    // 2.05 2단계 하부 디렉토리 설정 로직 추가
    memset(next_dir, 0x00, sizeof(next_dir));
	if (BS_GetProfileString("VBR Manager", "2ND SEARCH DIR", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("2ND SEARCH DIR is not defined.  Backup mode is standard.\n");
    }
    else
    {
    	setup_next_dir(buffer);
    }
    
    // 2.065 2단계 하부 디렉토리 백업 제외 로직 추가
    memset(next_dir, 0x00, sizeof(next_dir));
	if (BS_GetProfileString("VBR Manager", "EXCLUSION DIR", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("EXCLUSION DIR is not defined.  Backup mode is standard.\n");
    }
    else
    {
    	setup_exclusion_dir(buffer);
    }

	env_initialize();
	portal_initialize();
	
	BS_Tsyslog("NCC-VBR CIFS Manager ver %s start.\n", VERSION);

	sscanf(argv[1], "%s", src_directory);
	sscanf(argv[2], "%s", tgt_directory);
	
	BS_Tsyslog("VBR CIFS backup start.  source dir = [%s], target dir = [%s]\n", src_directory, tgt_directory);  
	TRAN_LOG3("VBR CIFS backup start.   source dir = [%s], target dir = [%s]", src_directory, tgt_directory);

	// 디렉토리 /를 _로 바꾸어야 이력 파일 생성 가능
	strcpy(src_directory_file, src_directory);
	point	=	src_directory_file;
	while(1)
	{
		if (*point == '/') *point = '_';
		if (*point == 0x00) break;
		point++;
	}
	strcpy(tgt_directory_file, tgt_directory);
	point	=	tgt_directory_file;
	while(1)
	{
		if (*point == '/') *point = '_';
		if (*point == 0x00) break;
		point++;
	}
	
	path_len = snprintf(history_file, sizeof(history_file), "./history/%s-%s.vbr",
	                    &src_directory_file[1], &tgt_directory_file[1]);
	if ((path_len < 0) || (path_len >= (int) sizeof(history_file)))
	{
		BS_Tsyslog("Backup history filename too long. source=[%s], target=[%s]\n",
		            src_directory, tgt_directory);
		exit(-1);
	}
	BS_Tsyslog("history file search.[%s]\n", history_file);	

    if ((fp = fopen(history_file, "r")) == NULL)
    {
        BS_Tsyslog("Backup history file [%s]is not found.\n", history_file);
        backup_time	= 0L;
    }
    else
    {
    	fgets(buffer, MAX_LENGTH, fp);
    	sscanf(buffer, "%s", backup_string);
    	backup_time		=	atol(backup_string);
	    fclose(fp);
    }

	time(&backup_start_time);
	lt_today = localtime(&backup_start_time);

	BS_Tsyslog("Backup base time  = [%ld]\n", backup_time);
	BS_Tsyslog("Backup start time = [%ld]\n", backup_start_time);
					 BS_Tsyslog("BEGIN : %ld [%04d-%02d-%02d %02d:%02d:%02d]\n", (long) backup_start_time,
					 		lt_today->tm_year + 1900,
                             lt_today->tm_mon + 1,
                             lt_today->tm_mday,
                             lt_today->tm_hour,
                             lt_today->tm_min,
                             lt_today->tm_sec);
	
	if (BS_Daemonize() == FALSE)
	{
		BS_Tsyslog("FATAL Error	[VBR Manager] STOP RUN(Daemonized fail).\n");
		printf("FATAL Error	[VBR Manager] STOP RUN(Daemonized fail).\n");
		exit(-1);
	}

	// log file max size setup
	if (BS_GetProfileString("VBR Manager", "MAX Log size", buffer, sizeof(buffer)) == FALSE)
    {
    	max_log_file_size	=	50000000L;
    	BS_Tsyslog("Config file[%s] [%s] parameter not found.(%ld default setting)..\n", CONFIG_FILE, "MAX Log size", max_log_file_size);
    }
    else
    {
    	max_log_file_size = atol(buffer);
    	BS_Tsyslog("CIFS Manager : Log File Max size is [%ld]\n", max_log_file_size);
    }

	// thread 0은 cifs_root 이므로 별도 처리함 (thread에서 처리하면 nas 파일이 적은 경우 cifs_root 처리 결과가 초기화 될 수 있으므로 별도 초기화 작업을 미리 수행
	cifs_thread[0].file_cnt			=	0L;
	cifs_thread[0].dir_cnt			=	0L;			
	cifs_thread[0].change_file_cnt	=	0L;
	cifs_thread[0].change_dir_cnt	=	0L;			

	BS_Tsyslog("VBR CIFS main thread search job start: %s=>%s.\n",  src_directory, tgt_directory);
	if (cifs_root(src_directory, tgt_directory)	!=	TRUE)
	{
		BS_Tsyslog("VBR TASK Internal error.  STOP RUN!!!\n");
		exit(-1);	
	}	
	BS_Tsyslog("VBR CIFS main thread finished: [%s]=>[%s]\n   Total File = [%lu], Total Dir = [%lu], Change file = [%lu], Create dir = [%lu]\n",  
	            src_directory, tgt_directory, cifs_thread[0].thread_file_cnt, cifs_thread[0].thread_dir_cnt, cifs_thread[0].thread_change_file_cnt, cifs_thread[0].thread_change_dir_cnt);

	if (thread_initialize() != TRUE)
	{
		BS_Tsyslog("Environment initialized	fail.  STOP RUN!!!\n");
		exit(-1);
	}
	
	while (1)
	{
		sleep(5);		
					// BS_Tsyslog("MIDDLE : %ld [%04d-%02d-%02d %02d:%02d:%02d]\n", (long) backup_start_time,
					// 		lt_today->tm_year + 1900,
                    //         lt_today->tm_mon + 1,
                    //         lt_today->tm_mday,
                    //         lt_today->tm_hour,
                    //         lt_today->tm_min,
                    //         lt_today->tm_sec);
		finish_cnt = 0;
		total_file = total_dir = change_file = change_dir = 0L;
//BS_Tsyslog("*****[%lu][%lu]\n", 			cifs_thread[0].file_cnt, cifs_thread[0].dir_cnt);
		for (i = 0; i <= MAX_CIFS_THREAD; i++)
		{
			if (cifs_thread[i].status != 'N' && cifs_thread[i].status != 'F')
			{
				// BS_Tsyslog("[%d] thread status is [%c][%d]\n", i, cifs_thread[i].status, finish_cnt);
			}
			else
			{
				finish_cnt++;
				total_file 		+= cifs_thread[i].file_cnt;
				total_dir 		+= cifs_thread[i].dir_cnt;
				change_file 	+= cifs_thread[i].change_file_cnt;
				change_dir 		+= cifs_thread[i].change_dir_cnt;			
			}
//BS_Tsyslog("*[%d] thread status is [%c][%d]*****[%lu][%lu] \n", i, cifs_thread[i].status, finish_cnt, cifs_thread[0].file_cnt, cifs_thread[0].dir_cnt);
			if (finish_cnt == MAX_CIFS_THREAD+1)
			{
    			if ((fp = fopen(history_file, "w")) == NULL)
    			{
    			    BS_Tsyslog("Backup history file write [%s]open error.\n", history_file);
    			}
    			else
    			{
    				fseek(fp, 0L, SEEK_SET);
					fprintf(fp, "%ld [%04d-%02d-%02d %02d:%02d:%02d]\n", (long) backup_start_time,
							lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec);
					BS_Tsyslog("END : %ld [%04d-%02d-%02d %02d:%02d:%02d]\n", (long) backup_start_time,
							lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec);
    			}
                fclose(fp);
                
                /* sk vbr 2.0671 B */
                if (msgctl(cifs_queue_id, IPC_RMID, NULL) == -1) {
        			BS_Tsyslog("Private Queue remove internal error[%d].....\n", cifs_queue_id);
      			} else {
	    			BS_Tsyslog("Private Queue remove success.....\n");
	    		}
                /* sk vbr 2.0671 E */
    			
				// BS_Tsyslog("VBR CIFS backup finished.  source dir = [%s], target dir = [%s]\n", src_directory, tgt_directory);
	   			// BS_Tsyslog("Total File = [%lu], Total Dir = [%lu], Change file = [%lu], Create dir = [%lu]\n", total_file, total_dir, change_file, change_dir);
	   			// exit(0);
//BS_Tsyslog("*******[%lu][%lu]\n", 			cifs_thread[0].file_cnt, cifs_thread[0].dir_cnt);
				BS_Tsyslog("VBR CIFS backup finished.  source dir = [%s], target dir = [%s]\n", src_directory, tgt_directory);
	   			TRAN_LOG3("VBR CIFS backup finished : source dir = [%s], target dir = [%s]", src_directory, tgt_directory);
	   			BS_Tsyslog("Total File = [%lu], Total Dir = [%lu], Change file = [%lu], Create dir = [%lu]\n", total_file, total_dir, change_file, change_dir);
	   			TRAN_LOG3("Total File = [%lu], Total Dir = [%lu], Change file = [%lu], Create dir = [%lu]", total_file, total_dir, change_file, change_dir);
	   			sprintf(buffer, "%.17s BACKUP: %s => %s Finished.", argv[3], argv[1], argv[2]);
				event_register('N', "CIFSFIN00", buffer);
				exit(0);	
			}
		}		
	}
	
}

int	env_initialize()
{

	/*
	if (BS_GetProfileString("VBR Manager", "QUEUE ID", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("Information ERROR. Config file[%s] [%s] parameter가 등록되지 않았습니다.\n", CONFIG_FILE, "QUEUE ID");
    	return FALSE;
    }
    cifs_msg_key	=	atoi(buffer);

	cifs_queue_id 			=	msgget(cifs_msg_key, IPC_CREAT | 0644);
	umask(022);
	*/
	
	/* SK vbr 2.0671 update B */
    BS_Tsyslog("Private Queue create mode.....\n");
    cifs_queue_id = msgget(IPC_PRIVATE, IPC_CREAT | 0644);
    if (cifs_queue_id == -1) {
        BS_Tsyslog("Private Queue create error.....STOP PROCESS.\n");
        exit(1);
    }

    BS_Tsyslog("Private Queue queue id : %d\n", cifs_queue_id);
	/* SK vbr 2.0671 update E */
	
	return	TRUE;
}

int	thread_initialize()
{
	int			curr_thread_cnt;
	char 		buffer[300];
	int			i;
	int			thr_id;

	curr_thread_cnt		=	MAX_CIFS_THREAD;		// default setup
	// BS_Tsyslog("VBR CIFS envisonment setup(THREAD) start.\n");	
	if (BS_GetProfileString("VBR Manager", "MAX Thread cnt", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("Config file[%s] [%s] parameter not found.(%d default setting)..\n", CONFIG_FILE, "MAX Thread cnt", MAX_CIFS_THREAD);
    }
    else
    {
    	curr_thread_cnt = atol(buffer);
    	if (curr_thread_cnt > MAX_CIFS_THREAD)
    	{
    		BS_Tsyslog("Thread cnt is %d, MAX Thread limit is %d, change thread cnt = %d.\n",  curr_thread_cnt, MAX_CIFS_THREAD, MAX_CIFS_THREAD);	
    		curr_thread_cnt = MAX_CIFS_THREAD;
    	}
    	else
    	{
    		BS_Tsyslog("CIFS Thread cnt = [%ld]\n", curr_thread_cnt);
    	}
    }

	for (i = 1; i <= MAX_CIFS_THREAD; i++)
	{
		if (i <= curr_thread_cnt)
		{
    		// 식별번호 p_thread 를 가지는 쓰레드를 detach
    		// 단 0은 main이므로 skip
			// BS_Tsyslog("[%d]th cifs thread starting.\n", i);
			thr_id	=	pthread_create(&cifs_thread_array[i], NULL, cifs_processing, 		(void *) &i);
    		if (thr_id < 0)
    		{
    		    BS_Tsyslog("CIFS thread create error : [%d]th - [%d].\n", i, thr_id);
    		    printf("CIFS thread create error : [%d]th - [%d].\n", i, thr_id);
    		    return FALSE;
    		}
			cifs_thread[i].status			=	'W';		
			sprintf(cifs_thread[i].last_time,  "0000-00-00 00:00:00");
			cifs_thread[i].file_cnt			=	0L;
			cifs_thread[i].dir_cnt			=	0L;			
			cifs_thread[i].change_file_cnt	=	0L;
			cifs_thread[i].change_dir_cnt	=	0L;			
			memset(cifs_thread[i].curr_file, 0x00, sizeof(cifs_thread[i].curr_file));
    		pthread_detach(cifs_thread_array[i]);
			sleep(1);
		}
		else
		{
			cifs_thread[i].status			=	'N';				
		}
	}
	return	TRUE;
}

int	directory_is_empty(char *dir_name)
{
	DIR			*dir;
	struct dirent	*entry;

	dir = opendir(dir_name);
	if (dir == NULL)
	{
		BS_Tsyslog("dis_ target directory open error[%s][%d][%s].\n", dir_name, errno, strerror(errno));
		return FALSE;
	}

	while ((entry = readdir(dir)) != NULL)
	{
		if ((strcmp(entry->d_name, ".") == 0) || (strcmp(entry->d_name, "..") == 0))
		{
			continue;
		}
		closedir(dir);
		return FALSE;
	}

	closedir(dir);
	return TRUE;
}

int	prepare_dis_target_dir(char *tgt_dir, char *dir_name)
{
	char			old_tgt_dir[MAX_FILE_LEN];
	char			new_tgt_dir[MAX_FILE_LEN];
	struct stat	old_tgt_stat;
	struct stat	new_tgt_stat;

	if ((strncmp(dir_name, "dis_", 4) != 0) || (strlen(dir_name) <= 4))
	{
		return TRUE;
	}

	if (snprintf(old_tgt_dir, sizeof(old_tgt_dir), "%s/%s", tgt_dir, &dir_name[4]) >= (int) sizeof(old_tgt_dir))
	{
		BS_Tsyslog("dis_ original target path is too long.[%s][%s].\n", tgt_dir, &dir_name[4]);
		return FALSE;
	}

	if (snprintf(new_tgt_dir, sizeof(new_tgt_dir), "%s/%s", tgt_dir, dir_name) >= (int) sizeof(new_tgt_dir))
	{
		BS_Tsyslog("dis_ target path is too long.[%s][%s].\n", tgt_dir, dir_name);
		return FALSE;
	}

	if (lstat(old_tgt_dir, &old_tgt_stat) < 0)
	{
		if (errno != ENOENT)
		{
			BS_Tsyslog("dis_ source directory target check error[%s][%d][%s].\n", old_tgt_dir, errno, strerror(errno));
			return FALSE;
		}
		return TRUE;
	}

	if (!S_ISDIR(old_tgt_stat.st_mode))
	{
		BS_Tsyslog("dis_ source directory target is not directory. Rename skip.[%s].\n", old_tgt_dir);
		return TRUE;
	}

	if (lstat(new_tgt_dir, &new_tgt_stat) == 0)
	{
		if (!S_ISDIR(new_tgt_stat.st_mode))
		{
			BS_Tsyslog("dis_ target path already exists and is not directory. Rename skip.[%s].\n", new_tgt_dir);
			return TRUE;
		}

		if (directory_is_empty(new_tgt_dir) != TRUE)
		{
			BS_Tsyslog("dis_ target directory already contains data. Rename skip.[%s]=>[%s].\n", old_tgt_dir, new_tgt_dir);
			return TRUE;
		}

		if (rmdir(new_tgt_dir) < 0)
		{
			BS_Tsyslog("dis_ empty target directory remove error[%s][%d][%s].\n", new_tgt_dir, errno, strerror(errno));
			return FALSE;
		}
	}
	else if (errno != ENOENT)
	{
		BS_Tsyslog("dis_ target directory check error[%s][%d][%s].\n", new_tgt_dir, errno, strerror(errno));
		return FALSE;
	}

	if (rename(old_tgt_dir, new_tgt_dir) < 0)
	{
		BS_Tsyslog("dis_ target directory rename error[%s]=>[%s][%d][%s].\n", old_tgt_dir, new_tgt_dir, errno, strerror(errno));
		return FALSE;
	}

	BS_Tsyslog("dis_ target directory rename success.[%s]=>[%s].\n", old_tgt_dir, new_tgt_dir);
	return TRUE;
}

int	cifs_root(char *src_dir, char *tgt_dir)
{    
	DIR 			*dir;
	struct stat 	buff;
	// char			file_type[MAX_FILE_LEN];
    // struct tm*  	lt_today;
	struct dirent 	*entry;
	char			file_name[MAX_FILE_LEN];
    // char			last_modify[200];
    Msg				msg;
    int				result;
    char			src_dirname[MAX_FILE_LEN];
    char			tgt_dirname[MAX_FILE_LEN];
    char			tgt_filename[MAX_FILE_LEN];
    char			temp_str[1000];
    int				i, next_loop, exclusion_find;
    
    dir = opendir (src_dir);
	if (dir == NULL)
	{
		BS_Tsyslog("Directory [%s][%d] is not valid.[%d][%s]!!!\n", src_dir, strlen(src_dir), errno, strerror(errno));
		return 	FALSE;				
	}
	cifs_thread[0].status			=	'A';		
	sprintf(cifs_thread[0].last_time,  "0000-00-00 00:00:00");
	// 재귀호출이 일어나므로 초기화는 main에서 (2.05)
	// cifs_thread[0].file_cnt			=	0L;
	// cifs_thread[0].dir_cnt			=	0L;			
	// cifs_thread[0].change_file_cnt	=	0L;
	// cifs_thread[0].change_dir_cnt	=	0L;			
	memset(cifs_thread[0].curr_file, 0x00, sizeof(cifs_thread[0].curr_file));

	while ((entry = readdir (dir)) != NULL)
	{
		next_loop		=	FALSE;
		exclusion_find	=	FALSE;
//BS_Tsyslog("***[%lu][%lu]\n", 			cifs_thread[0].file_cnt, cifs_thread[0].dir_cnt);
		if ((strcmp(entry->d_name, ".") == 0) || (strcmp(entry->d_name, "..") == 0))
		{
			continue;
		}
		strcpy(temp_str, entry->d_name);
		if (strcmp(&temp_str[1], "snapshot") == 0)
		{
			BS_Tsyslog("snapshot skip.\n");
			continue;
		}
		sprintf(file_name, "%s/%s", src_dir, entry->d_name);
		if (lstat(file_name, &buff) < 0)
		{
			BS_Tsyslog("lstat internal error[%s][%d][%s]!!!\n", file_name, errno, strerror(errno));
			continue;
		}
		if (S_ISREG(buff.st_mode))
		{
			cifs_thread[0].file_cnt++;
			cifs_thread[0].thread_file_cnt++;
			sprintf(tgt_filename, "%s/%s", tgt_dir, entry->d_name);
			if ((result = file_check(file_name, tgt_filename)) == FALSE)
			{
				BS_Tsyslog("VBR CIFS file copy error.[%s][%s]\n", file_name, tgt_filename);
			}
			if (result == CHANGE)
			{
				cifs_thread[0].change_file_cnt++;
				cifs_thread[0].thread_change_file_cnt++;
				// BS_Tsyslog("FILE change..	[%s][%s]\n", file_name, tgt_filename);
			}
		}
		else if (S_ISDIR(buff.st_mode))
		{
			// list_search(file_name);
			// strcpy(file_type, 	"Directory");
			cifs_thread[0].dir_cnt++;
			cifs_thread[0].thread_dir_cnt++;

			if (exclusion_dir_mode	==	TRUE)		// exclusion mode enable status
			{
				sprintf(src_dirname, "%s/%s", src_dir, entry->d_name);
				for (i = 0; i < MAX_2ND_DIR; i++)
				{
					if (strcmp(src_dirname, exclusion_dir[i]) == 0)
					{
						BS_Tsyslog("Exclusion directory found. SKIP directory. [%d] : SRC = [%s], TGT = [%s].   \n", i, src_dirname, exclusion_dir[i]);
						exclusion_find	=	TRUE;
						break;
					}
				}
			}

			if ((exclusion_find != TRUE) && (strcmp(src_dir, src_directory) == 0) && (strncmp(entry->d_name, "dis_", 4) == 0))
			{
				if (prepare_dis_target_dir(tgt_dir, entry->d_name) != TRUE)
				{
					BS_Tsyslog("dis_ target preparation failed. Continue normal backup.[%s/%s].\n", tgt_dir, entry->d_name);
				}
			}

			if (next_dir_mode	==	TRUE)		// 2nd search mode enable status
			{
				sprintf(src_dirname, "%s/%s", src_dir, entry->d_name);
				for (i = 0; i < MAX_2ND_DIR; i++)
				{
//BS_Tsyslog("[%d] : SRC = [%s], TGT = [%s]\n", i, src_dirname, next_dir[i]);
					if (strcmp(src_dirname, next_dir[i]) == 0)
					{
						sprintf(tgt_dirname, "%s/%s", tgt_dir, entry->d_name);		// 20180727
						// cifs_root(src_dirname, next_dir[i]);
						cifs_root(src_dirname, tgt_dirname);
						cifs_thread[0].status			=	'A';		// 재귀호출에서 thread 상태가 'F'로 바뀌므로 원상태로
						next_loop	=	TRUE;
						break;
					}
					if (strlen(next_dir[i]) == 0)
					{
						break;
					}
				}
			}
			
			if (next_loop		==	TRUE)	continue;		// 2nd search mode && 해당 dir가 검색된 경우 재귀호출이 실행되었으므로 이하 로직은 실행되면 안됨
			if (exclusion_find	==	TRUE)	continue;		// Exclusion directory는 백업이 되면 안되므로 skip
				
			memset(&msg, 0x00, sizeof(msg));
			msg.msg_type		=	5;
//			strcpy(msg.msg_text, entry->d_name);
			sprintf(msg.msg_text, "%s/%s^&^%s/%s", src_dir, entry->d_name, tgt_dir, entry->d_name);
// BS_Tsyslog("src[%s]tgt[%s]file[%s]msg[%s]\n", src_dir, tgt_dir, entry->d_name, msg.msg_text);
			result	=	msgsnd(cifs_queue_id, &msg, strlen(msg.msg_text), 0);
			if (result == -1)
			{
				BS_Tsyslog("CIFS TASK Queue id %d, send ERROR [%d][%s]\n", cifs_queue_id, errno, strerror(errno));
				return 	FALSE;				
			}
BS_Tsyslog("VBR BACKUP TASK registered=>[%s]\n", msg.msg_text);
		}
		else
		{
			BS_Tsyslog("Contents [%s] Unknown type.\n",  file_name);	
		}
	}
	closedir (dir);
	cifs_thread[0].status			=	'F';		
//BS_Tsyslog("===[%lu][%lu]\n", 			cifs_thread[0].file_cnt, cifs_thread[0].dir_cnt);
	return TRUE;
}

int	file_check(char *src, char *tgt)
{
	struct stat 	src_buff;
	struct stat 	tgt_buff;
	int				change;
	int				new_file;
//	int				file_exist;
	char			bak_filename[MAX_FILE_LEN];
	char			temp[MAX_FILE_LEN];
	int				fd;
// 20190327    gzFile  		*zfp;
    gzFile  		zfp;
    char			buffer[CIFS_BUFFER];
    int				n;
    int				comp_flag;
    int				filename_len;
	
	change	=	new_file	=	FALSE;
	
	if (lstat(src, &src_buff) < 0)
	{
		BS_Tsyslog("FILE COPY : lstat internal error[%s][%d][%s]!!!\n", src, errno, strerror(errno));
		return FALSE;
	}
	
	/* Target 파일이 없으면 source 파일 시간과 관계없이 신규 복사 */
	if (lstat(tgt, &tgt_buff) < 0)
	{
		if (errno == ENOENT)
		{
			if (file_copy(src, tgt) != TRUE)
			{
				return FALSE;
			}
			return CHANGE;
		}

		BS_Tsyslog("FILE COPY : lstat internal error[%s][%d][%s]!!!\n", tgt, errno, strerror(errno));
		return FALSE;
	}

	// 기준 시간보다 빠르면 skip (1.2)
	// windows7에서 일부 text file이 최종 access time 변경되지 않아 date 전체 비교  (2.04)
	if ((src_buff.st_atime <= backup_time) && (src_buff.st_mtime <= backup_time) && (src_buff.st_ctime <= backup_time))
	{
		// BS_Tsyslog("file [%s] atime = %ld, 기준시간 = %ld.  skip.\n", src, src_buff.st_atime, backup_time);
		return	TRUE;
	}
	
	if (src_buff.st_size != tgt_buff.st_size)
	{
			change	=	TRUE;
	}
	
	if (src_buff.st_mtime > tgt_buff.st_mtime)	
	{
			change	=	TRUE;
	}
	
	if (change == FALSE)
	{
		return	TRUE;
	}

	if (new_file	==	FALSE)		// 기존 파일 변경
	{
		BS_date_time_no_delimiter2(temp);
		filename_len = snprintf(bak_filename, sizeof(bak_filename), "%s-vbr-%s.zip", tgt, temp);
		if ((filename_len < 0) || (filename_len >= (int) sizeof(bak_filename)))
		{
			BS_Tsyslog("FILE COPY : backup filename too long[%s][%s].\n", tgt, temp);
			return FALSE;
		}
		// 일반 파일 사본 저장
		// if (rename(tgt, bak_filename) < 0)
		// {
		// 	BS_Tsyslog("FILE COPY : backup file compress internal error[%s][%s][%d][%s]!!!\n", tgt, bak_filename, errno, strerror(errno));
		// 	return FALSE;
		// }
					
		// 압축 파일 사본 저장
    	if ((fd = open(tgt, O_RDONLY)) < 0)
    	{
    	    BS_Tsyslog("FILE COPY : old file open internal error[%s][%s][%d][%s]!!!\n", tgt, bak_filename, errno, strerror(errno));
    	}
    	if ((zfp = gzopen(bak_filename, "wb")) == NULL)
    	{
    	    BS_Tsyslog("FILE COPY : backup file create internal error[%s][%s][%d][%s]!!!\n", tgt, bak_filename, errno, strerror(errno));
    	}
    	gzsetparams(zfp, 5, Z_DEFAULT_STRATEGY);
    	comp_flag = TRUE;		// 백업 압축 성공 기본 설정
    	while((n = read(fd, buffer, CIFS_BUFFER)) > 0)
    	{
    	    if (gzwrite(zfp, buffer, n) < 0)
    	    {
    	    	BS_Tsyslog("FILE COPY : backup file write internal error[%s][%s][%d][%s]!!!\n", tgt, bak_filename, errno, strerror(errno));
    	    	comp_flag = FALSE;		// 백업 압축 실패
    	        break;
    	    }
    	}
    	gzclose(zfp);
    	close(fd);
    	
    	if (comp_flag != TRUE)
    	{
    		if (remove(bak_filename) < 0)
    		{
    	    	BS_Tsyslog("FILE COPY : backup temp file remove internal error[%s][%s][%d][%s]!!!\n", tgt, bak_filename, errno, strerror(errno));    			
    		}    		
    	}
    	else
    	{
			// BS_Tsyslog2("OLD File Save : Old File[%s] => Compress save[%s].\n", tgt, bak_filename);    		
    	}
	}
	// BS_Tsyslog("Change File Copy : SRC[%s] => TGT[%s].\n", src, tgt);
	if (file_copy(src, tgt) != TRUE)
	{
		return FALSE;
	}
	return	CHANGE;
}

int	file_copy(char *src, char *tgt)
{
	FILE	*s, *t;
	char	buffer[CIFS_BUFFER];
	int		read_len;
	
	if ((s = fopen(src, "rb")) == NULL)
    {
        BS_Tsyslog("VBR CIFS source file[%s] READ ONLY open error.[%d][%s]\n", src, errno, strerror(errno));
        return FALSE;    
    }
	if ((t = fopen(tgt, "wb")) == NULL)
    {
        BS_Tsyslog("VBR CIFS target file[%s] open error.[%d][%s]\n", tgt, errno, strerror(errno));
		fclose(s);
        return FALSE;    
    }
    
    
    while((read_len = fread(buffer, sizeof(char), CIFS_BUFFER, s)) > 0)
    {
        if (fwrite(buffer, sizeof(char), read_len, t) < 0)
        {
        	BS_Tsyslog("VBR CIFS file copy[%s]=>[%s] error.[%d][%s]\n", src, tgt, errno, strerror(errno));
        	fclose(s);fclose(t);
        	return FALSE;    
        }
    }
    // BS_Tsyslog2("File Copy : SRC[%s] => TGT[%s]\n", src, tgt);

    fclose(s);fclose(t);    
	return	TRUE;
}

int dir_check(char *tgt)
{
	struct stat 	tgt_buff;

	// BS_Tsyslog("Directory TGT[%s].\n", tgt);
	if (lstat(tgt, &tgt_buff) < 0)
	{
		if (errno == 2)	//No such file or directory
		{
			if (mkdir(tgt, 0755) < 0)
			{
				BS_Tsyslog("Directory Create internal error[%s][%d][%s]!!!\n", tgt, errno, strerror(errno));
				return	FALSE;				
			}
			else
			{
				return	CHANGE;				
			}
		}
		else
		{
			BS_Tsyslog("Directory Check : lstat internal error[%s][%d][%s]!!!\n", tgt, errno, strerror(errno));
			return FALSE;
		}
	}
	else
	{
		return	TRUE;	
	}
}

int	file_find(char *tgt_file)
{
	return	TRUE;
}

void *cifs_processing(void *args)			// Sync type 전문 처리
{
	int			thread_seq;
	int			nbytes;
	Msg			msg;
    char		src_dirname[MAX_FILE_LEN];
    char		tgt_dirname[MAX_FILE_LEN];
    char		*p;
    int			index;
	
	thread_seq				=	*((int *)args);
//	thread_seq				=	(int) (int *)args;
//	BS_Tsyslog("VBR CIFS thread[%d]th start.\n", thread_seq);
	cifs_thread[thread_seq].status			=	'A';		

	while(1)
	{
		memset(&msg, 0x00, sizeof(msg));
		// 해당 thread에서 처리한 dir에서 처리된 파일, 디렉토리, 변경분민 따로 출력을 위하여  (2.05)
		cifs_thread[thread_seq].thread_file_cnt	=	0;
		cifs_thread[thread_seq].thread_dir_cnt	=	0;
		cifs_thread[thread_seq].thread_change_file_cnt	=	0;
		cifs_thread[thread_seq].thread_change_dir_cnt	=	0;
		
		nbytes 	=	msgrcv(cifs_queue_id, &msg, MAX_DATA_SIZE, 0, IPC_NOWAIT);
		if (nbytes <= 0)
		{
			cifs_thread[thread_seq].status			=	'F';
			BS_Tsyslog("VBR CIFS thread [%d]th finished.\n", thread_seq);
			break;
		}
		BS_Tsyslog("VBR CIFS thread [%d]th job start: [%s]\n",  thread_seq, msg.msg_text);
//		sprintf(src_dirname, "%s/%s", src_directory, msg.msg_text);
//		sprintf(tgt_dirname, "%s/%s", tgt_directory, msg.msg_text);
		// 공백(space)이 있는 directory의 경우 fail - 보완 (2.01)
		// source dir과 target dir 사이에 구분자 (^\^)
		// sscanf( msg.msg_text, "%s %s", src_dirname, tgt_dirname);
		p = msg.msg_text;
		index = 0;
		memset(src_dirname, 0x00, sizeof(src_dirname));
		memset(tgt_dirname, 0x00, sizeof(tgt_dirname));
		while(*p)
		{
			if (*p == '^' && *(p+1) == '&' && *(p+2) == '^')
			{
				memcpy(src_dirname, msg.msg_text, index);
				p+=3;
				strcpy(tgt_dirname, p);
				break;
			}
			p++; index++;
		}
// BS_Tsyslog("VBR CIFS worker thread [%d]th start: [%s]=>[%s]\n",  thread_seq, src_dirname, tgt_dirname);		
		if (cifs_sub(thread_seq, src_dirname, tgt_dirname) != TRUE)
		{
//			BS_Tsyslog("VBR CIFS thread [%d]th internal error: [%s]\n",  thread_seq, msg.msg_text);
			BS_Tsyslog("VBR CIFS thread [%d]th internal error: [%s]=>[%s]\n",  thread_seq, src_dirname, tgt_dirname);
		}
		else
		{
			BS_Tsyslog("VBR CIFS thread [%d]th finished: [%s]=>[%s]\n   Total File = [%lu], Total Dir = [%lu], Change file = [%lu], Create dir = [%lu]\n",  
			            thread_seq, src_dirname, tgt_dirname, cifs_thread[thread_seq].thread_file_cnt, cifs_thread[thread_seq].thread_dir_cnt, cifs_thread[thread_seq].thread_change_file_cnt, cifs_thread[thread_seq].thread_change_dir_cnt);
		}
	}
	
	return	NULL;
}

int	cifs_sub(int thread_seq, char *src_dirname, char *tgt_dirname)
{    
	DIR 			*dir;
	struct stat 	buff;
//    struct tm*  	lt_today;
	struct dirent 	*entry;
//    char			last_modify[200];
//    Msg				msg;
    int				result;
    char			src_filename[MAX_FILE_LEN];
    char			tgt_filename[MAX_FILE_LEN];
	char			temp_str[1000];
// BS_Tsyslog("DEBUG start [%s][%s]\n", src_dirname, tgt_dirname);	
    dir = opendir (src_dirname);
	if (dir == NULL)
	{
		BS_Tsyslog("Directory [%s][%d] is not valid.[%d][%s]!!!\n", src_dirname, strlen(src_dirname), errno, strerror(errno));
		return 	FALSE;				
	}

	// target home directory check
	if ((result = dir_check(tgt_dirname)) == FALSE)
	{
		BS_Tsyslog("VBR CIFS target home directory create error.[%s][%s]\n", tgt_dirname);
		return FALSE;
	}
	if (result == CHANGE)
	{
		cifs_thread[thread_seq].change_dir_cnt++;
		cifs_thread[thread_seq].thread_change_dir_cnt++;
		//BS_Tsyslog("Directory create.	[%s]\n", tgt_filename);
	}

	while ((entry = readdir (dir)) != NULL)
	{
		if ((strcmp(entry->d_name, ".") == 0) || (strcmp(entry->d_name, "..") == 0))
		{
			continue;
		}
		strcpy(temp_str, entry->d_name);
		if (strcmp(&temp_str[1], "snapshot") == 0)
		{
			BS_Tsyslog("snapshot skip.\n");
			continue;
		}
		sprintf(src_filename, "%s/%s", src_dirname, entry->d_name);
// BS_Tsyslog("DEBUG [%s]\n", src_filename);	
		if (lstat(src_filename, &buff) < 0)
		{
			BS_Tsyslog("lstat internal error[%s][%d][%s]!!!\n", src_filename, errno, strerror(errno));
			continue;
		}
		if (S_ISREG(buff.st_mode))
		{
			
			sprintf(tgt_filename, "%s/%s", tgt_dirname, entry->d_name);
			cifs_thread[thread_seq].file_cnt++;
			cifs_thread[thread_seq].thread_file_cnt++;

			if ((result = file_check(src_filename, tgt_filename)) == FALSE)
			{
				BS_Tsyslog("VBR CIFS file copy error.[%s][%s]\n", src_filename, tgt_filename);
				continue;
			}
			if (result == CHANGE)
			{
				//BS_Tsyslog("FILE change.	[%s][%s]\n", src_filename, tgt_filename);
				cifs_thread[thread_seq].change_file_cnt++;
				cifs_thread[thread_seq].thread_change_file_cnt++;
			}
		}
		else if (S_ISDIR(buff.st_mode))
		{
			sprintf(tgt_filename, "%s/%s", tgt_dirname, entry->d_name);
			// BS_Tsyslog("Directory COPY start: [%s][%s]\n", src_filename, tgt_filename);
			cifs_thread[thread_seq].dir_cnt++;
			cifs_thread[thread_seq].thread_dir_cnt++;
			if ((result = dir_check(tgt_filename)) == FALSE)
			{
				BS_Tsyslog("VBR CIFS directory copy error.[%s][%s]\n", src_filename, tgt_filename);
				return FALSE;
			}
			if (result == CHANGE)
			{
				cifs_thread[thread_seq].change_dir_cnt++;
				cifs_thread[thread_seq].thread_change_dir_cnt++;
				//BS_Tsyslog("Directory create.	[%s]\n", tgt_filename);
			}
			// cifs_sub(entry->d_name);
			cifs_sub(thread_seq, src_filename, tgt_filename);
			// strcpy(file_type, 	"Directory");
		}
		else
		{
			BS_Tsyslog("[DEBUG] Contents [%s] Unknown type.\n",  src_filename);	
		}
		//lt_today = localtime(&buff.st_mtime);
		//				
    	//sprintf(last_modify,"%04d-%02d-%02d %02d:%02d:%02d",
        //                    lt_today->tm_year + 1900,
        //                    lt_today->tm_mon + 1,
        //                    lt_today->tm_mday,
        //                    lt_today->tm_hour,
        //                    lt_today->tm_min,
        //                    lt_today->tm_sec);				
		//printf("type:%.10s,size:%ld,name:%s,last_update:%s\n", 
		//	                 file_type, buff.st_size, file_name, last_modify);
	}
	closedir (dir);
	return TRUE;
}

int	event_register(char req_event_type, char *req_event_code, char *req_event_msg)
{
	int		i;
	int		result;
	// status => '0' : 초기화, '1' : 미송신, '9' : 송신 완료	
	for (i = 0; i < MAX_EVENT_MAX; i++)
	{
		//동일한 경우는 overwrite
		if ((memcmp(event_msg[i].event_code, req_event_code, strlen(req_event_code)) == 0) &&
			(event_msg[i].status == '1'))
		{
			BS_date_time(event_msg[i].event_time);
BS_Tsyslog("Time update[%d]\n", i);
			return	TRUE;
		}
		if (event_msg[i].status != '1')
		{
			event_msg[i].status			=	'1';
			event_msg[i].event_type		=	req_event_type;
			BS_date_time(event_msg[i].event_time);
			strcpy(event_msg[i].event_code, req_event_code);
			strcpy(event_msg[i].event_msg, 	req_event_msg);
BS_Tsyslog("event insert[%d]\n", i);
			if ((result = send_portal(i)) == TRUE)
			{
				BS_Tsyslog("Event [%s][%s] Send to VBR WEB Portal success.\n", 	req_event_code, req_event_msg);
				event_msg[i].status			=	'9';
			}
			else
			{
				BS_Tsyslog("Event [%s][%s] Send to VBR WEB Portal failed.\n", 	req_event_code, req_event_msg);				
			}
			return	TRUE;
		}
	}
	return FALSE;
}

int	send_portal(int i)
{
	int 		sock;
	struct 		sockaddr_in serv_addr;	
	int			result;
//	char		*ptr;
	char		temp[1000];
	char		send_data[5000];
	BACKUP_INFO	backup_msg;
	//SK

	if (portal_info != TRUE) return FALSE;

	sock = socket(PF_INET, SOCK_STREAM,	0);
	memset(&serv_addr, 0, sizeof(serv_addr));
	serv_addr.sin_family=AF_INET;
	// serv_addr.sin_addr.s_addr=inet_addr("192.168.1.18");
	// serv_addr.sin_port=htons(10021);
	serv_addr.sin_addr.s_addr=inet_addr(portal_ip);
	serv_addr.sin_port=htons(portal_port);

	if(connect(sock, (struct sockaddr*)&serv_addr, sizeof(serv_addr))==-1)
	{
		BS_Tsyslog("WEB Portal Manager is not alive or check ip[%s], port[%d]...\n", portal_ip, portal_port);
		return FALSE;
	}

	sprintf(temp, "{\"event_info\":[");
	strcpy(backup_msg.msg_text, temp);
	
	sprintf(temp, "{\"event_type\":\"%c\",\"host_name\":\"%s\",\"host_uuid\":\"%s\",\"event_time\":\"%s\",\"event_code\":\"%s\",\"event_msg\":\"%s\"}",
	                event_msg[i].event_type, "VBR CIFS", "NONE", event_msg[i].event_time, event_msg[i].event_code, event_msg[i].event_msg);
	strcat(backup_msg.msg_text, temp);
	strcat(backup_msg.msg_text, "]}");
	
	backup_msg.msg_type		=	'B';
	BS_date_time_no_delimiter2(backup_msg.msg_seq);
	strcat(backup_msg.msg_seq, "001");
	backup_msg.task_type	=	'0';
	memcpy(backup_msg.timeout, "000", sizeof(char)*3);
	backup_msg.priority		=	'1';
	
	memset(send_data, 0x00, sizeof(send_data));
	sprintf(send_data, "%07d", (int) strlen((char *)&backup_msg.msg_type));
	memcpy(&send_data[7], &backup_msg.msg_type, strlen((char *)&backup_msg.msg_type));

//BS_Tsyslog("Before send to VBR WEB Portal manager[%s]\n", send_data);
	
    if ((result = write(sock,&send_data, strlen(send_data))) < 0)     		
    {
		BS_Tsyslog("send to VBR WEB Portal manager => Failed.[%d][%s]\n", errno, send_data);
		return FALSE;
	}
	else
	{
		BS_Tsyslog("send to VBR WEB Portal manager => Success.[%s]\n", send_data);
	}
 		
	if ((result = read(sock, send_data, sizeof(send_data) - 1)) < 0)
    {
		BS_Tsyslog("recv from VBR WEB Portal manager => Failed.[%d]\n", errno);
	}
	else
 	{
		send_data[result] = 0x00;
 		BS_Tsyslog("recv from VBR WEB Portal manager => Success.\n");
 		BS_Tsyslog("=====   RESULT   =====\n%s", send_data);
 
	}
	return	TRUE;
}

int portal_initialize()
{
	char buffer[100];

	memset(buffer, 0x00, sizeof(buffer));
	// log file max size setup
	if (BS_GetProfileString("VBR Manager", "WEB server(IP)", buffer, sizeof(buffer)) == FALSE)
    {
    	portal_info	=	FALSE;
    	BS_Tsyslog("Warning. Config file[%s] [%s] parameter not found.\n", CONFIG_FILE, "WEB server(IP)");
    	return FALSE;
    }
    strcpy(portal_ip, buffer);
    
	if (BS_GetProfileString("VBR Manager", "WEB server(PORT)", buffer, sizeof(buffer)) == FALSE)
    {
    	portal_info	=	FALSE;
    	BS_Tsyslog("Warning. Config file[%s] [%s] parameter not found.\n", CONFIG_FILE, "WEB server(PORT)");
    	return FALSE;
    }
    portal_port	=	atoi(buffer);
    portal_info	=	TRUE;
    BS_Tsyslog("WEB Portal ip = %s, port = %d\n", portal_ip, portal_port);
    return TRUE;
}

int	setup_next_dir(char *setup_string)
{
	char	*next_dir_string;
	int		i;
	
	next_dir_mode	=	FALSE;
	next_dir_string	= strtok(setup_string, " ");
	while (next_dir_string != NULL)
	{
//		BS_Tsyslog("2ND SEARCH DIR = [%s]\n", next_dir_string);
		for (i = 0; i < MAX_2ND_DIR; i++)
		{
			if (strlen(next_dir[i]) == 0)
			{
				BS_Tsyslog("Special backup policy (2nd search directory) [%d]th [%s] setup.\n", i, next_dir_string);
				strcpy(next_dir[i], next_dir_string);
				break;
			}
		}
		if (i >= MAX_2ND_DIR)
		{
			BS_Tsyslog("MAX 2ND SEARCH DIR overflow. change to standard backup mode. [%d]\n", i);
			return FALSE;
		}
		next_dir_string	= strtok(NULL, " ");
	}
	// BS_Tsyslog("2ND SEARCH DIR setup string = [%s]\n", setup_string);
	next_dir_mode	=	TRUE;
	
	return	TRUE;
}

int	setup_exclusion_dir(char *setup_string)
{
	char	*next_exclusion_string;
	int		i;
	
	exclusion_dir_mode	=	FALSE;
	next_exclusion_string	= strtok(setup_string, " ");
	while (next_exclusion_string != NULL)
	{
		BS_Tsyslog("exclusion DIR = [%s]\n", next_exclusion_string);
		for (i = 0; i < MAX_2ND_DIR; i++)
		{
			if (strlen(exclusion_dir[i]) == 0)
			{
				BS_Tsyslog("Special backup policy (exclusion directory) [%d]th [%s] setup.\n", i, next_exclusion_string);
				strcpy(exclusion_dir[i], next_exclusion_string);
				break;
			}
		}
		if (i >= MAX_2ND_DIR)
		{
			BS_Tsyslog("Exclusion DIR overflow. change to standard backup mode. [%d]\n", i);
			return FALSE;
		}
		next_exclusion_string	= strtok(NULL, " ");
	}
	BS_Tsyslog("exclusion DIR setup string = [%s]\n", setup_string);
	exclusion_dir_mode	=	TRUE;
	
	return	TRUE;
}
