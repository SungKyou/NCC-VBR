#include		"bs_common.h"
#define			MAX_EVENT_MAX				30
#define			MAX_2ND_DIR					10
#define			VERSION						"1.000"
#define			PGM_NAME					"File Remove (BATCH) for NCC-VBR"


// 1.000		한국장학재단 고객사 file 삭제 batch 신규 개발

typedef struct
{
	char 			status;
	char			curr_file[MAX_FILE_LEN];
	// 해당 thread에서 검색 정보 누적 관리, 삭제 종료시 thread 정보를 합산하여 관리
	unsigned long	file_cnt;
	unsigned long	skip_cnt;
	unsigned long	dir_cnt;
	unsigned long	delete_file_cnt;
	// SK 현재 백업중인 sub dir 정보 별도 관리, 해당 sub dir 종료되면 clear
	unsigned long	thread_file_cnt;
	unsigned long	thread_skip_cnt;
	unsigned long	thread_dir_cnt;
	unsigned long	thread_delete_file_cnt;
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
	char 			msg_type;						// message type, VBR : 'B'로 고정
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
char			src_directory[MAX_FILE_LEN], delete_days[256];	// delete directory. delete base days
EVENT_MSG		event_msg[2];
int  			portal_info;					// portal 정보 setup 확인 (TRUE/FALSE)
char			portal_ip[100];					// portal ip address
int				portal_port;					// portal listen port
long			backup_time;
time_t			backup_start_time;
struct	tm*		lt_today;

// 2.05
// 1단계 하부 디렉토리 정책 삭제가 아닌 2단계까지 내려가는 하부 디렉토리 삭제 대상 폴더 (파일 개수가 많아 삭제 병렬처리가 추가로 필요한 디렉토리 정의 (node_info.cfg에 정의)
char 			next_dir[MAX_2ND_DIR][512];
char 			exclusion_dir[MAX_2ND_DIR][512];
int				next_dir_mode;			// 2단계 삭제 모드 TRUE/FALSE
int				exclusion_dir_mode;		// 특정 폴더 삭제 제외 모드 TRUE/FALSE

char			rm_date_time[MAX_FILE_LEN];		// 삭제 기준일
char			delete_mode;			// '0' : LOG Mode, '9' : Delete mode

void *cifs_processing(void *);
int	setup_next_dir(char *);
int	setup_exclusion_dir(char *);
int	env_initialize();
int	cifs_root(char *);
int	thread_initialize();
int	file_check(char *);
int	file_delete(char *);
int	cifs_sub(int , char *);
int check_rm_date(int , char *);

int	main(int argc, char	*argv[])
{
	char 		buffer[1024];
	int			i, finish_cnt;
	// int			thr_id;
	unsigned long	total_file 		= 0L;
	unsigned long	skip_file 		= 0L;
	unsigned long	total_dir		= 0L;
	unsigned long	delete_file 	= 0L;
	char			backup_string[1024];
	FILE			*fp;
	char			*point;

	if ((argc == 2)	&& ((strcmp(argv[1], "-version") ==	0) || (strcmp(argv[1], "-ver") == 0)))
	{
		printf("%s version	= %s\n", PGM_NAME, VERSION);
		exit(TRUE);
	}

	BS_initialize();

	// log file	max	size setup
	if (BS_GetProfileString(PGM_NAME, "MAX Log size", buffer, sizeof(buffer)) == FALSE)
	{
		max_log_file_size	=	50000000L;
		BS_Tsyslog("Config file[%s] [%s] parameter가 등록되지 않았습니다. %ld byte로 초기 설정합니다.\n", CONFIG_FILE, "MAX Log size", max_log_file_size);
	}
	else
	{
		max_log_file_size =	atol(buffer);
		BS_Tsyslog("Log File 최대 설정값 %ld byte 적용합니다.\n", max_log_file_size);
	}

	if (argc != 3)		// PGM_NAME Delete-dir days
	{
		printf("Usage : %s Delete-dir days\n", argv[0]);
		BS_Tsyslog("%s execute parameter : internal error.[%d]\n", argv[0], argc);  		
		BS_Tsyslog("Usage : %s Delete-dir days\n", argv[0]);
		exit(-1);
	}

	// Delete mode setup
	if (BS_GetProfileString(PGM_NAME, "Delete Mode", buffer, sizeof(buffer)) == FALSE)
	{
		BS_Tsyslog("Config file[%s] [%s] parameter가 등록되지 않았습니다. LOG mode로 기본 설정합니다.\n",	CONFIG_FILE, "Delete Mode");
		delete_mode	=	'0';
	}
	else
	{
		if (strcmp("DELETE", buffer) == 0)
		{
			BS_Tsyslog("%s File DELETE mode로 실행합니다.\n", PGM_NAME);		
			delete_mode	=	'9';
		}
		else
		{
			BS_Tsyslog("%s LOG mode로 실행합니다.\n", PGM_NAME);		
			delete_mode	=	'0';			
		}
	}
    // 2단계 하부 디렉토리 설정 로직 추가 (대량 파일이 저장된 sub dir인 경우 삭제가 병렬 처리되도록)
    memset(next_dir, 0x00, sizeof(next_dir));
	if (BS_GetProfileString(PGM_NAME, "2ND SEARCH DIR", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("2ND SEARCH DIR is not defined.  File Delete mode is standard.\n");
    }
    else
    {
    	setup_next_dir(buffer);
    }
    
    // 하부 디렉토리 삭제 제외 로직 추가
    memset(next_dir, 0x00, sizeof(next_dir));
	if (BS_GetProfileString(PGM_NAME, "EXCLUSION DIR", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("EXCLUSION DIR is not defined. File Delete mode is standard.\n");
    }
    else
    {
    	setup_exclusion_dir(buffer);
    }

	env_initialize();
	
	BS_Tsyslog("%s ver %s start.\n", PGM_NAME, VERSION);

	sscanf(argv[1], "%s", src_directory);
	sscanf(argv[2], "%s", delete_days);
	
	BS_Tsyslog("%s start.  delete dir = [%s], base(delete) days = [%s]\n", PGM_NAME, src_directory, delete_days);  

	check_rm_date(atoi(delete_days), rm_date_time);		// 삭제 기준일 설정 (현재 일자 기준 삭제 기준일 설정)
	// 기존 delete log file이 있는 경우 미리 삭제
	remove(DELETE_FILE);	
		
	if (BS_Daemonize() == FALSE)
	{
		BS_Tsyslog("FATAL Error	: %s STOP RUN(Daemonized fail).\n", PGM_NAME);
		printf("FATAL Error	: %s STOP RUN(Daemonized fail).\n", PGM_NAME);
		exit(-1);
	}

	// thread 0은 cifs_root 이므로 별도 처리
	cifs_thread[0].file_cnt			=	0L;
	cifs_thread[0].skip_cnt			=	0L;
	cifs_thread[0].dir_cnt			=	0L;			
	cifs_thread[0].delete_file_cnt	=	0L;

	BS_Tsyslog("%s main thread search job start: %s %s.\n", PGM_NAME, src_directory, rm_date_time);
	if (cifs_root(src_directory)	!=	TRUE)
	{
		BS_Tsyslog("%s TASK register : Internal error.  STOP RUN!!!\n", PGM_NAME);
		exit(-1);	
	}	
	BS_Tsyslog("%s main thread finished: [%s] [%s]\n   Total File = [%lu], Total Dir = [%lu], Delete file = [%lu]\n",  
	            PGM_NAME, src_directory, rm_date_time, cifs_thread[0].thread_file_cnt, cifs_thread[0].thread_dir_cnt, cifs_thread[0].thread_delete_file_cnt,cifs_thread[0].thread_skip_cnt);

	if (thread_initialize() != TRUE)
	{
		BS_Tsyslog("Environment initialized	fail.  STOP RUN!!!\n");
		exit(-1);
	}

	while (1)
	{
		sleep(1);		
		finish_cnt = 0;
		total_file = total_dir = delete_file = 0L;
		
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
				skip_file 		+= cifs_thread[i].skip_cnt;
				total_dir 		+= cifs_thread[i].dir_cnt;
				delete_file 	+= cifs_thread[i].delete_file_cnt;
			}
			if (finish_cnt == MAX_CIFS_THREAD+1)
			{
				BS_Tsyslog("NCC-VBR batch finished.  Directory = [%s], delete base time = [%s]\n", src_directory, rm_date_time);
	   			BS_Tsyslog("Total File = [%lu], Total Dir = [%lu], Delete files = [%lu] Skip files = [%lu]\n", total_file, total_dir, delete_file, skip_file);
				exit(0);	
			}
		}		
	}
	
}

int	env_initialize()
{
	char	buffer[MAX_BUFFER_SIZE];

	if (BS_GetProfileString(PGM_NAME, "QUEUE ID", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("Information ERROR. Config file[%s] [%s] parameter가 등록되지 않았습니다.\n", CONFIG_FILE, "QUEUE ID");
    	return FALSE;
    }
    cifs_msg_key	=	atoi(buffer);

	cifs_queue_id 			=	msgget(cifs_msg_key, IPC_CREAT | 0644);
	umask(022);
	return	TRUE;
}

int	thread_initialize()
{
	int			curr_thread_cnt;
	char 		buffer[300];
	int			i;
	int			thr_id;

	curr_thread_cnt		=	MAX_CIFS_THREAD;		// default setup
	if (BS_GetProfileString(PGM_NAME, "MAX Thread cnt", buffer, sizeof(buffer)) == FALSE)
    {
    	BS_Tsyslog("Config file[%s] [%s] parameter not found.(%d default setting)..\n", CONFIG_FILE, "MAX Thread cnt", MAX_CIFS_THREAD);
    }
    else
    {
    	curr_thread_cnt = atol(buffer);
    	if (curr_thread_cnt > MAX_CIFS_THREAD)
    	{
    		BS_Tsyslog("%s Thread cnt is %d, MAX Thread limit is %d, change thread cnt = %d.\n", PGM_NAME, curr_thread_cnt, MAX_CIFS_THREAD, MAX_CIFS_THREAD);	
    		curr_thread_cnt = MAX_CIFS_THREAD;
    	}
    	else
    	{
    		BS_Tsyslog("%s Thread cnt = [%ld]\n", PGM_NAME, curr_thread_cnt);
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
			cifs_thread[i].skip_cnt			=	0L;
			cifs_thread[i].dir_cnt			=	0L;			
			cifs_thread[i].delete_file_cnt	=	0L;
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

int	cifs_root(char *src_dir)
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
    char			delete_filename[MAX_FILE_LEN];
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
	// 재귀호출이 일어나므로 초기화는 main에서
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
			if ((result = file_check(file_name)) == FALSE)
			{
				BS_Tsyslog("%s file check error.[%s]\n", PGM_NAME, file_name);
			}
			if (result == CHANGE)
			{
				cifs_thread[0].delete_file_cnt++;
				cifs_thread[0].thread_delete_file_cnt++;
			}
			// BS_Tsyslog("%s file check finished.[%s]\n", PGM_NAME, file_name);
		}
		else if (S_ISDIR(buff.st_mode))
		{
			cifs_thread[0].dir_cnt++;
			cifs_thread[0].thread_dir_cnt++;

			if (exclusion_dir_mode	==	TRUE)		// exclusion mode enable status
			{
				sprintf(src_dirname, "%s/%s", src_dir, entry->d_name);
				for (i = 0; i < MAX_2ND_DIR; i++)
				{
					if (strcmp(src_dirname, exclusion_dir[i]) == 0)
					{
						BS_Tsyslog("Exclusion directory found. SKIP directory. [%d] : exclusion dir = [%s]\n", i, src_dirname);
						exclusion_find	=	TRUE;
						break;
					}
				}
			}

			if (next_dir_mode	==	TRUE)		// 2nd search mode enable status
			{
				sprintf(src_dirname, "%s/%s", src_dir, entry->d_name);
				for (i = 0; i < MAX_2ND_DIR; i++)
				{
					if (strcmp(src_dirname, next_dir[i]) == 0)
					{
						cifs_root(src_dirname);
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
			if (exclusion_find	==	TRUE)	continue;		// Exclusion directory는 delete가 되면 안되므로 skip
				
			memset(&msg, 0x00, sizeof(msg));
			msg.msg_type		=	5;
			sprintf(msg.msg_text, "%s/%s", src_dir, entry->d_name);
			result	=	msgsnd(cifs_queue_id, &msg, strlen(msg.msg_text), 0);
			if (result == -1)
			{
				BS_Tsyslog("CIFS TASK Queue id %d, send ERROR [%d][%s]\n", cifs_queue_id, errno, strerror(errno));
				return 	FALSE;				
			}
			BS_Tsyslog("%s TASK registered=>[%s]\n", PGM_NAME, msg.msg_text);
		}
		else
		{
			BS_Tsyslog("Contents => [%s] Unknown type.\n",  file_name);	
		}
	}
	closedir (dir);
	cifs_thread[0].status			=	'F';		
	return TRUE;
}

int	file_check(char *src)
{
	struct stat 	src_buff;
	struct stat 	tgt_buff;
	int				change;
	int				new_file;
	char			bak_filename[MAX_FILE_LEN];
	char			temp[MAX_FILE_LEN];
	int				fd;
    gzFile  		zfp;
    char			buffer[CIFS_BUFFER];
    int				n;
    int				comp_flag;
	
	change	=	new_file	=	FALSE;
	
	if (lstat(src, &src_buff) < 0)
	{
		BS_Tsyslog("lstat internal error[%s][%d][%s]!!!\n", src, errno, strerror(errno));
		return FALSE;
	}
	
	return (file_delete(src));
}

int	file_delete(char *src)
{
	char	buffer[CIFS_BUFFER];
	int		file_len, result;
	
	//BS_Tsyslog("DELETE check [%s]\n", src);
	file_len = strlen(src);

	if ((strcmp(&src[file_len-4], ".zip") != 0)	|| (file_len < 24) || (strncmp(&src[file_len-23], "-vbr-", 5) != 0) || (strncmp(&src[file_len-18], rm_date_time, 14) > 0))
	{
		return	TRUE;
	}
	// BS_Tsyslog("***** Check file [%d][%s]\n", file_len, src);	
	
	if (delete_mode == '9')
	{
		result	=	remove(src);
		if (result == -1)
		{
			return	FALSE;
		}
		BS_Tsyslog("File Name [%s] : Delete finished.\n", src);
	}
	else	
	{
		BS_Delete(src);		// delete file write	
		// BS_Tsyslog("File Name [%s] : Delete log file append finished.\n", src);
	}
	return	CHANGE;
}

void *cifs_processing(void *args)			// Sync type 전문 처리
{
	int			thread_seq;
	int			nbytes;
	Msg			msg;
    char		src_dirname[MAX_FILE_LEN];
    char		*p;
    int			index;
	
	thread_seq				=	*((int *)args);
	cifs_thread[thread_seq].status			=	'A';		

	while(1)
	{
		memset(&msg, 0x00, sizeof(msg));
		cifs_thread[thread_seq].thread_file_cnt	=	0;
		cifs_thread[thread_seq].thread_skip_cnt	=	0;
		cifs_thread[thread_seq].thread_dir_cnt	=	0;
		cifs_thread[thread_seq].thread_delete_file_cnt	=	0;
		
		nbytes 	=	msgrcv(cifs_queue_id, &msg, MAX_DATA_SIZE, 0, IPC_NOWAIT);
		if (nbytes <= 0)
		{
			cifs_thread[thread_seq].status			=	'F';
			BS_Tsyslog("%s thread [%d]th finished.\n", PGM_NAME, thread_seq);
			break;
		}
		BS_Tsyslog("%s thread [%d]th job start: [%s]\n", PGM_NAME, thread_seq, msg.msg_text);
		p = msg.msg_text;
		index = 0;
		memset(src_dirname, 0x00, sizeof(src_dirname));
		memcpy(src_dirname, msg.msg_text, nbytes);

		if (cifs_sub(thread_seq, src_dirname) != TRUE)
		{
			BS_Tsyslog("%s thread [%d]th internal error: [%s]\n", PGM_NAME, thread_seq, src_dirname);
		}
		else
		{
			BS_Tsyslog("%s thread [%d]th finished: [%s]\n   Total File = [%lu], Total Dir = [%lu], Delete file = [%lu], Skip file = [%lu]\n",  
			            PGM_NAME, thread_seq, src_dirname, cifs_thread[thread_seq].thread_file_cnt, cifs_thread[thread_seq].thread_dir_cnt, cifs_thread[thread_seq].thread_delete_file_cnt, cifs_thread[thread_seq].thread_skip_cnt);
		}
	}
	
	return	NULL;
}

int	cifs_sub(int thread_seq, char *src_dirname)
{    
	DIR 			*dir;
	struct stat 	buff;
	struct dirent 	*entry;
    int				result;
    char			src_filename[MAX_FILE_LEN];
    char			tgt_filename[MAX_FILE_LEN];
	char			temp_str[1000];

    dir = opendir (src_dirname);
	if (dir == NULL)
	{
		BS_Tsyslog("Directory [%s][%d] is not valid.[%d][%s]!!!\n", src_dirname, strlen(src_dirname), errno, strerror(errno));
		return 	FALSE;				
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
		if (lstat(src_filename, &buff) < 0)
		{
			BS_Tsyslog("lstat internal error[%s][%d][%s]!!!\n", src_filename, errno, strerror(errno));
			continue;
		}
		if (S_ISREG(buff.st_mode))
		{
			cifs_thread[thread_seq].file_cnt++;
			cifs_thread[thread_seq].thread_file_cnt++;

			if ((result = file_check(src_filename)) == FALSE)
			{
				BS_Tsyslog("%s file check error.[%s]\n", PGM_NAME, src_filename);
				continue;
			}
			if (result == CHANGE)
			{
				cifs_thread[thread_seq].delete_file_cnt++;
				cifs_thread[thread_seq].thread_delete_file_cnt++;
			}
			// BS_Tsyslog("%s file check finished.[%s]\n", PGM_NAME, src_filename);
		}
		else if (S_ISDIR(buff.st_mode))
		{
			cifs_thread[thread_seq].dir_cnt++;
			cifs_thread[thread_seq].thread_dir_cnt++;
			cifs_sub(thread_seq, src_filename);
		}
		else
		{
			BS_Tsyslog("[DEBUG] Contents [%s] Unknown type.\n",  src_filename);
			cifs_thread[thread_seq].file_cnt++;
			cifs_thread[thread_seq].thread_file_cnt++;
			cifs_thread[thread_seq].skip_cnt++;				
			cifs_thread[thread_seq].thread_skip_cnt++;
		}
	}
	closedir (dir);
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
				BS_Tsyslog("Special Delete policy (2nd search directory) [%d]th [%s] setup.\n", i, next_dir_string);
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

int check_rm_date(int rm_day, char *rm_date_time)
{
    struct tm*  lt_today;
	char		date_time[100];
    time_t      lt_time;

    time(&lt_time);
    lt_today = localtime(&lt_time);

    sprintf(date_time,     "%04d-%02d-%02d %02d:%02d:%02d",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec);
	BS_Tsyslog("Current base date = %s\n", date_time);

	lt_time	-=		3600*24*rm_day;
    lt_today = localtime(&lt_time);

    sprintf(date_time,     "%04d-%02d-%02d %02d:%02d:%02d",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec);

    sprintf(rm_date_time,   "%04d%02d%02d%02d%02d%02d",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec);
                            
	BS_Tsyslog("Delete base date = %s\n", date_time);
}