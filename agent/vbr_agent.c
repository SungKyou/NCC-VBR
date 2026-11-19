#include		"bs_common.h"
#define MAX_THREAD_CNT  	10
#define	MAX_MSG_CNT			100
// for PoC only (추후 config file에서 설정)
#define			MAX_SYNC_THREAD_CNT			10
#define			MIN_SYNC_THREAD_CNT			2
#define			MAX_ASYNC_THREAD_CNT		10
#define			MIN_ASYNC_THREAD_CNT		2
#define			MAX_MGT_THREAD_CNT			10
#define			MIN_MGT_THREAD_CNT			2
#define			DEFAULT_SYNC_THREAD_CNT		3
#define			DEFAULT_ASYNC_THREAD_CNT	3
#define			DEFAULT_MGT_THREAD_CNT		2
#define			MAX_VBD_CNT					15
#define			MAX_MSG_RETRY				5
#define			HEALTH_CHECK_INTERVAL		10
#define			MAX_EVENT_CNT				30
#define			VERSION						"1.602"

#define 		TAR_BLOCK_SIZE 				512
#define 		BUFFER_SIZE 				(1024 * 1024)  // 1MB buffer
#define 		DISK_BLOCK_SIZE 			1048576  // 1MB

// 1.531 		STATUS call시 version 정보가 나오지 않는 부분 수정 (current_version variable setting)
// 1.532		ALIVE.chk에 hostname 추가
// 1.602		xva file 검증 로직 추가 (20260108)

/* internal struct define */
typedef struct
{
	int				status;						// 현재 처리 상태 (0: 미사용/처리완료, 1: 미처리, '3': QUEUE 송신, '5': 결과 송신 완료, '9': 내부 오류)
	int				fd;							// 송신 서버의 fd 저장 (응답 처리용)
	int				msg_len;					// 전문 전체 길이
	long			queue_seq;					// Queue 저장 unique sequence
	char 			msg_type;					// message type, Backup의 경우 'B'로 고정
	char			msg_seq[17];				// message sequence  YYYYMMDDHHMISS + 1/1000sec
	char			task_type;					// 송수신 전문 task 구분
	char			timeout[3];					// Sync 처리 timeout value (sec, 0 : 무한 대기)
	char			priority;					// 처리 우선 순위 (0 : 즉시, 5 : 일반, 9 : 다른 task 우선 처리)
	char			msg_text[MAX_QUEUE_LEN];	// 원격 송신 자료, 처리 결과 자료
} BACKUP_INFO;

typedef struct {
	long 	msg_type;
	char 	msg_text[MAX_QUEUE_LEN];
} Msg;

typedef struct
{
	int		timeout;
	char	comm_text[MAX_QUEUE_LEN-8];	
} DUMMY_MSG;

typedef struct
{
	char			run_flag[10];				// 'A'ctive, 'S'tandby, 'D'elete
	struct timeval  start_time;					// task 실행 start 시간
	char			last_time[20];				// 최종 업데이트 일시
	char			last_time2[15];				// 최종 업데이트 일시 (delimiter 없음)
	int				sec;						// 처리소요시간 초
	long			msec;						// 처리소요시간 1/1000000
	char			msg_text[MAX_QUEUE_LEN];
} SYNC_THREAD;

typedef struct
{
	char			run_flag[10];				// 'A'ctive, 'S'tandby, 'D'elete, 'P'ending
	struct timeval  start_time;					// task 실행 start 시간
	char			last_time[20];				// 최종 업데이트 일시
	char			last_time2[15];				// 최종 업데이트 일시 (delimiter 없음)
	int				sec;						// 처리소요시간 초
	long			msec;						// 처리소요시간 1/1000000
	char			msg_seq[17];				// 처리중인 task
	char			msg_text[MAX_QUEUE_LEN];
} ASYNC_THREAD;

typedef struct
{
	char			run_flag[10];				// 'A'ctive, 'S'tandby, 'D'elete
	struct timeval  start_time;					// task 실행 start 시간
	char			last_time[20];				// 최종 업데이트 일시
	char			last_time2[15];				// 최종 업데이트 일시 (delimiter 없음)
	int				sec;						// 처리소요시간 초
	long			msec;						// 처리소요시간 1/1000000
	char			msg_text[MAX_QUEUE_LEN];
} MGT_THREAD;

typedef struct
{
	char			xenserver[10];				// 'A'ctive, 'F'ail
	int				disk_usage;					// /영역 사용율
	char			storage[10];				// 스토리지 상태 : 'A'ctive, 'F'ail
	float			cpu_usage;					// dom 0  cpu usage (3초 간격)
	float			mem_usage;					// dom 0 memory usage
	char			last_time[20];				// 최종 업데이트 일시
} HEALTH_CHECK;

typedef struct
{
	char			status;						// '0' : 초기화, '1' : 미송신, '9' : 송신 완료
	char			event_type;					// 'C'Critical, 'W'Warning
	char			event_time[20];				// event 발생 시간
	char			event_code[10];				// event code (TBD)
	char			event_msg[200];				// event text message
} EVENT_MSG;

// TAR 헤더 구조체
typedef struct __attribute__((packed)) {
    char name[100];
    char mode[8];
    char uid[8];
    char gid[8];
    char size[12];
    char mtime[12];
    char chksum[8];
    char typeflag;
    char linkname[100];
    char magic[6];
    char version[2];
    char uname[32];
    char gname[32];
    char devmajor[8];
    char devminor[8];
    char prefix[155];
    char padding[12];
} tar_header_t;

typedef struct {
    int is_valid;
    char error_msg[256];
    long long file_size;
    int has_ova_xml;
    int disk_count;
    int total_files;
    int regular_files;
    int directories;
    int is_compressed;
    
    char last_file_name[256];
    int last_file_is_xxhash;
    long long last_disk_block_number;
    long long calculated_disk_size;
    int validation_error;
} xva_validation_result_t;

/* 전역변수 */
/* si_ 변수군은 bs_common.h header file에 공통 정의 */
int 			total_recv_len;					// 현재 처리중인 전문의 전체 길이 (함수간 프로세싱을 위해 내부 전역 변수 선언)
FD_INFO			fd_info[MAX_CLIENT_CNT];		// TCP socket 접속 fd 저장
BACKUP_INFO*	backup_info[MAX_MSG_CNT];		// 송수신 전문 저장
int				curr_seq;						// 현재 처리중인 message seq no.
void* 			shared_memory = (void*)0;		// shared memory start address
SYNC_THREAD		sync_thread[MAX_SYNC_THREAD_CNT];
ASYNC_THREAD	async_thread[MAX_ASYNC_THREAD_CNT];
MGT_THREAD		mgt_thread[MAX_MGT_THREAD_CNT];
HEALTH_CHECK	health_check;
EVENT_MSG		event_msg[30];
int				current_queue_seq;				// 취소 전문 처리(message queue 삭제)를 위한 unique queue sequence id
char			current_version[100];			// 버전 관리용
float			cpu_limit	=	0.0f;			// 백업/복구 TASK 실행시 CPU 제한 수치 (cfg file에서 읽어옴)						
char			g_host_name[100];					// xenserver hostname
char			g_host_uuid[100];					// xenserver uuid

int  			portal_info;					// portal 정보 setup 확인 (TRUE/FALSE)
char			portal_ip[100];					// portal ip address
int				portal_port;					// portal listen port

/* function prototype */
static int 			MessageProcessing();
void 				signal_handler(int	sig);
static int			backup_processing(int seq);
void 				*backup_receiver(void *args);
void 				*backup_sync_sender(void *args);
void 				*backup_async_sender(void *args);
void 				*backup_query_sender(void *args);
void 				*sync_processing(void *args);			// Sync type 전문 처리
void 				*async_processing(void *args);			// Async type 전문 처리
void 				*mgt_processing(void *args);			// Query/Cancel/Thread/Inquire type 전문 처리
void 				*health_check_processing(void *args);	// Health check thread
void 				*monitor(void *args);					// internal monitoring thread
int 				backup_vm(int, BACKUP_INFO *backup);			// VM export
int 				import_vm(int, BACKUP_INFO *backup);			// VM import 
int					sequence_handler(int);					// unique queue id handler
int					event_register(char, char *, char *);

/* Message QUEUE define */
key_t			sync_msg_key		=	10001;		// (Sync)
key_t			async_msg_key		=	10005;		// (Async)
key_t			mgt_msg_key			=	10009;		// (Management)
key_t			sync_recv_msg_key	=	10001;
key_t			sync_send_msg_key	=	20001;
key_t			async_recv_msg_key	=	10005;
key_t			async_send_msg_key	=	20005;
key_t			mgt_recv_msg_key	=	10009;
key_t			mgt_send_msg_key	=	20009;
key_t			dummy_recv_msg_key	=	30001;
//key_t			dummy_send_msg_key	=	30002;
int				dummy_recv_queue_id;
//int				dummy_send_queue_id;
// thread별 unique key field - dummy에 요청할 떄 기본 값이며, thread 값을 add해서 구분하도록
long			main_value			=	1000L;
long			sync_value			=	2000L;
long			async_value			=	3000L;
long			mgt_value			=	4000L;
long			health_value		=	5000L;
long			env_value			=	8000L;


int	main(int argc, char	**argv)
{
	struct			sigaction	act;	
	int				state;
	pthread_t		thread[MAX_THREAD_CNT];
	pthread_t		sync_thread_array[MAX_SYNC_THREAD_CNT];
	pthread_t		async_thread_array[MAX_ASYNC_THREAD_CNT];
	pthread_t		mgt_thread_array[MAX_MGT_THREAD_CNT];
	int				i;
	int 			result;
	int				thr_id;
	char			buffer[1000];

	if ((argc == 2) && ((strcmp(argv[1], "-version") == 0) || (strcmp(argv[1], "-ver") == 0)))
	{
		printf("Xenserver Agent for NCC-VBR version = %s\n", VERSION);
		exit(0);
	}

	BS_initialize();
	portal_initialize();
	// log file max size setup
	if (BS_GetProfileString("VBR Agent for Xenserver", "MAX Log size", buffer, sizeof(buffer)) == FALSE)
    {
    	max_log_file_size	=	50000000L;
    	BS_Tsyslog("FATAL Error. Config file[%s] [%s] parameter not found.(%ld default setting)..\n", CONFIG_FILE, "MAX Log size", max_log_file_size);
    }
    else
    {
    	max_log_file_size = atol(buffer);
    	BS_Tsyslog("Log File Max size is [%ld]\n", max_log_file_size);
    }

	if (BS_Daemonize() == FALSE)
	{
		BS_Tsyslog("FATAL Error	[VBR Agent for Xenserver] STOP RUN(Daemonize).\n");
		printf("FATAL Error	[VBR Agent for Xenserver] STOP RUN(Daemonize).\n");
		exit(-1);
	}

	signal(SIGPIPE,	SIG_IGN);	
	act.sa_handler	=	signal_handler;
	sigemptyset(&act.sa_mask);
	act.sa_flags	=	0;

	state			=	sigaction(SIGCHLD, &act, 0);
	if (state != 0)
	{
		BS_Tsyslog("FATAL Error	[VBR Agent for Xenserver] STOP RUN(SIGCHLD setup).\n");
		printf("FATAL Error	[VBR Agent for Xenserver] STOP RUN(SIGCHLD setup).\n");
		exit(-1);
	}
	/*
	state			=	sigaction(SIGALRM, &act, 0);	
	if (state != 0)
	{
		BS_Tsyslog("FATAL Error	[VBR Agent for Xenserver] STOP RUN(SIGALRM setup).\n");
		printf("FATAL Error	[VBR Agent for Xenserver] STOP RUN(SIGALRM setup).\n");
		exit(-1);
	}
	*/
	
	env_initialize();	

	result = BS_NetworkListen("VBR Agent for Xenserver", "PORT-NO");
	if (result 	==	FALSE)
	{
		BS_Tsyslog("FATAL Error	[VBR Agent] STOP RUN(Network listen).\n");
		printf("FATAL Error	[VBR Agent] STOP RUN(Network listen).\n");
		exit(-1);
	}
	if (result 	==	EXIST)
	{
		BS_Tsyslog("VBR Agent is already running.\n");
		printf("VBR Agent is already running.\n");
		exit(-1);
	}

	pthread_create(&thread[0], NULL, backup_receiver,			(void *) 0);
	pthread_create(&thread[1], NULL, backup_sync_sender,		(void *) 0);
	pthread_create(&thread[2], NULL, backup_async_sender,		(void *) 0);
	pthread_create(&thread[3], NULL, backup_query_sender,		(void *) 0);
	pthread_create(&thread[4], NULL, health_check_processing,	(void *) 0);
	pthread_create(&thread[5], NULL, monitor,					(void *) 0);
	for (i = 0; i < MAX_SYNC_THREAD_CNT; i++)
	{
		if (i < DEFAULT_SYNC_THREAD_CNT)
		{
			BS_Tsyslog("[%d]th sync thread starting.(1 sec waiting.)\n", i);
			thr_id	=	pthread_create(&sync_thread_array[i], NULL, sync_processing, 		(void *) &i);
    		if (thr_id < 0)
    		{
    		    BS_Tsyslog("Sync thread create error : [%d]th - [%d].\n", i, thr_id);
    		    printf("Sync thread create error : [%d]th - [%d].\n", i, thr_id);
    		    exit(0);
    		}
			strcpy(sync_thread[i].run_flag,    "Standby");		
			sprintf(sync_thread[i].last_time,  "0000-00-00 00:00:00");
			sprintf(sync_thread[i].last_time2, "00000000000000");
			sync_thread[i].sec				=	0;
			sync_thread[i].msec				=	0L;			
			memset(sync_thread[i].msg_text, 0x00, sizeof(sync_thread[i].msg_text));
    		// 식별번호 p_thread 를 가지는 쓰레드를 detach 
    		pthread_detach(sync_thread_array[i]);
			sleep(1);
		}
		else
		{
			sync_thread[i].run_flag[0]		=	'N';				
		}
	}
	for (i = 0; i < MAX_ASYNC_THREAD_CNT; i++)
	{
		if (i < DEFAULT_ASYNC_THREAD_CNT)
		{
			BS_Tsyslog("[%d]th async thread starting.(1 sec waiting.)\n", i);
			thr_id	=	pthread_create(&async_thread_array[i], NULL, async_processing, 		(void *) &i);
    		if (thr_id < 0)
    		{
    		    BS_Tsyslog("Async thread create error : [%d]th - [%d].\n", i, thr_id);
    		    printf("Async thread create error : [%d]th - [%d].\n", i, thr_id);
    		    exit(0);
    		}
			strcpy(async_thread[i].run_flag,    "Standby");		
			sprintf(async_thread[i].last_time,  "0000-00-00 00:00:00");
			sprintf(async_thread[i].last_time2, "00000000000000");
			async_thread[i].sec				=	0;
			async_thread[i].msec			=	0L;			
			memset(async_thread[i].msg_seq,	 0x00, sizeof(async_thread[i].msg_seq));
			memset(async_thread[i].msg_text, 0x00, sizeof(async_thread[i].msg_text));
    		// 식별번호 p_thread 를 가지는 쓰레드를 detach 
    		pthread_detach(async_thread_array[i]);
			sleep(1);
		}
		else
		{
			async_thread[i].run_flag[0]		=	'N';				
		}
	}
	for (i = 0; i < MAX_MGT_THREAD_CNT; i++)
	{
		if (i < DEFAULT_MGT_THREAD_CNT)
		{
			BS_Tsyslog("[%d]th management thread starting.(1 sec waiting.)\n", i);
			thr_id	=	pthread_create(&mgt_thread_array[i], NULL, mgt_processing, 		(void *) &i);
    		if (thr_id < 0)
    		{
    		    BS_Tsyslog("Management thread create error : [%d]th - [%d].\n", i, thr_id);
    		    printf("Management thread create error : [%d]th - [%d].\n", i, thr_id);
    		    exit(0);
    		}
			strcpy(mgt_thread[i].run_flag,    "Standby");		
			sprintf(mgt_thread[i].last_time,  "0000-00-00 00:00:00");
			sprintf(mgt_thread[i].last_time2, "00000000000000");
			mgt_thread[i].sec				=	0;
			mgt_thread[i].msec				=	0L;			
			memset(mgt_thread[i].msg_text, 0x00, sizeof(mgt_thread[i].msg_text));
    		// 식별번호 p_thread 를 가지는 쓰레드를 detach 
    		pthread_detach(mgt_thread_array[i]);
			sleep(1);
		}
		else
		{
			mgt_thread[i].run_flag[0]		=	'N';				
		}
	}
	BS_Tsyslog("VBR agent for Xenserver service start.\n");
	sprintf(current_version, "NCC-VBR v%s", VERSION);
	BS_Tsyslog("NCC-VBR version	: [%s]\n", VERSION);

	RealTimeMain();
}

int	env_initialize()
{
	int		shmid, i, return_value;
	int		first_run;						// VBR agent가 최초 기동인지 재기동인지 확인
	FILE	*fp;
	char	host_uuid_text[100]		=	"uuid ( RO)          : ";
	char	name_label_text[100]	=	"    name-label ( RW): ";
	char	buffer[MAX_BUFFER_SIZE];
	char	command[100];
	// shmid = shmget((key_t) 1,sizeof(struct shared_use_st) * 100 ,0666 | IPC_CREAT);    
	shmid = shmget((key_t) 1,sizeof(BACKUP_INFO) * MAX_MSG_CNT, 0666 | IPC_CREAT | IPC_EXCL);    
	
	BS_Tsyslog("Environment setup start.\n");
	
	//공유메모리생성
	if(shmid == -1){
		if (errno == 17)	// shared memory는 생성되어 있고, agent만 재기동 된 경우
		{
	        BS_Tsyslog("Shared memory exist.\n");    
			first_run	=	FALSE;
	        shmid = shmget((key_t)1,sizeof(BACKUP_INFO) * 100 ,0666 | IPC_CREAT);
	        BS_Tsyslog("Shared memory reuse success....\n");
		}
		else
		{
	        BS_Tsyslog("Shared memory create error. errno = [%d], error message = [%s]\n", errno, strerror(errno));
	        printf("Shared memory create error. errno = [%d], error message = [%s]\n", errno, strerror(errno));
	        exit(1);
	    }
	}
	else
	{
		first_run	=	TRUE;
	    BS_Tsyslog("Shared memory creation success....\n");
	    sequence_handler(1);			// 최초 실행시 1로 초기 설정    
	
	}
	shared_memory	=	(void *) shmat(shmid,(void*)0,0);
	if (shared_memory == (void*)-1)
	{
	        BS_Tsyslog("Shared_memory location Error.   STOP RUN.\n");
	        printf("Shared_memory location Error.   STOP RUN.\n");
	        exit(1);
	}	

	// dummy process와 통신을 위한 공통 queue 생성
//	dummy_recv_queue_id 			=	msgget(dummy_recv_msg_key, IPC_EXCL | 0644);
	dummy_recv_queue_id 			=	msgget(dummy_recv_msg_key, IPC_EXCL | IPC_CREAT| 0644);
	if (dummy_recv_queue_id < 0)
	{
		BS_Tsyslog("Communication queue already exist.\n");
		dummy_recv_queue_id 			=	msgget(dummy_recv_msg_key, IPC_CREAT | 0644);			
		msgctl(dummy_recv_queue_id, IPC_RMID, 0);	
		BS_Tsyslog("Communication queue recreate...\n");
		dummy_recv_queue_id 			=	msgget(dummy_recv_msg_key, IPC_CREAT | 0644);			
	}
//	dummy_send_queue_id 			=	msgget(dummy_send_msg_key, IPC_CREAT | 0644);
	
	health_check.xenserver[0]	=	'#';
	health_check.storage[0]	=	'#';
	sprintf(health_check.last_time, "0000-00-00 00:00:00");
	health_check.disk_usage	=	0;
	health_check.cpu_usage	=	0.0f;

	for	(i = 0;	i <	MAX_CLIENT_CNT;	i++) 
	{
		fd_info[i].flag		=	0;
		fd_info[i].fd 		=	0;
		memset(fd_info[i].ip_address, 0x00,	sizeof(fd_info[i].ip_address));
	}

	memset(buffer,	0x00,	sizeof(buffer));
	sprintf(buffer, "%s", "hostname");
	dummy_call(env_value, HEALTH_CHECK_INTERVAL, buffer);

	BS_Tsyslog("HOSTNAME = [%s]\n", buffer);
	if (strlen(buffer) <= 0)
	{
		BS_Tsyslog("Xenserver hostname internal ERROR!!!!  STOP RUN.\n");	
		exit(-1);
	}
	else
	{
		strcpy(g_host_name, buffer);
	}
	/* hostname과 uuid값이 return 되지 않아 종료되는 문제 개선 중
	memset(command,	0x00,	sizeof(command));
	sprintf(command, "./dummy %d \"%s%s\" 2>&1", HEALTH_CHECK_INTERVAL, "xe host-list params=uuid,name-label hostname=", g_host_name);
	fp = popen(command, "r");
	BS_Tsyslog("Sleep before 2 sec\n");
	sleep(2);
	BS_Tsyslog("Sleep after 2 sec\n");
	i = return_value = 0;
	while(1)
	{
		memset(buffer, 0x00, sizeof(buffer));
		fgets(buffer, sizeof(buffer), fp);
		BS_Tsyslog("READ [%s]\n", buffer);
		if (strlen(buffer) == 0)
		{
			if (return_value == 0)
			{
				if (i++	>= 5)
				{
					BS_Tsyslog("host name && uuid check error.  STOP RUN\n");
					exit(-1);
				}
				BS_Tsyslog("[%d] internal wait for check hostname, uuid\n", i);
				sleep(1);
				continue;
			}
			else
			{
				if (feof(fp))
				{
					break;	
				}				
			}
		}
		else
		{
			return_value = 1;
			if (feof(fp))
			{
				break;	
			}
		}
		if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
		{
			BS_Tsyslog("Xenserver is not available.... STOP RUN!!!\n");
			printf("Xenserver is not available.... STOP RUN!!!\n");
			exit(-1);
		}
		BS_Tsyslog("BUFF = [%s][%s][%s]\n", host_uuid_text, name_label_text, buffer);
		if (memcmp(buffer, host_uuid_text, strlen(host_uuid_text)) == 0)
		{
			strcpy(g_host_uuid, &buffer[strlen(host_uuid_text)]);
			g_host_uuid[strlen(g_host_uuid)-1] = 0x00;
			BS_Tsyslog("Xenserver Host UUID=[%s]\n", g_host_uuid);
		}
		if (memcmp(buffer, name_label_text, strlen(name_label_text)) == 0)
		{
			strcpy(g_host_name, &buffer[strlen(name_label_text)]);
			g_host_name[strlen(g_host_name)-1] = 0x00;
			BS_Tsyslog("Xenserver Host Name=[%s]\n", g_host_name);
		}
		sleep(1);
	}
	pclose(fp);
	*/
	
	if ((fp	=	fopen(XENSERVER_INVENTORY, "r")) == NULL)
	{
		BS_Tsyslog("Xenserver Config file open ERROR!!!!  STOP RUN.[%s]\n", XENSERVER_INVENTORY);	
		printf("Xenserver Config file open ERROR!!!!  STOP RUN.[%s]\n", XENSERVER_INVENTORY);	
		exit(-1);		
	}
	memset(g_host_uuid, 0x00, sizeof(g_host_uuid));
	while(fgets(buffer, sizeof(buffer), fp)) {
		if (memcmp(buffer, "INSTALLATION_UUID='", strlen("INSTALLATION_UUID='")) == 0)
		{
			memcpy(g_host_uuid, &buffer[strlen("INSTALLATION_UUID='")], 36);
			break;
		}
	}
	fclose(fp);
	if ((strlen(g_host_name) <= 0) || (strlen(g_host_uuid) != 36))
	{
		BS_Tsyslog("Xenserver hostname/UUID internal ERROR!!!!  STOP RUN.[%s][%s]\n", g_host_name, g_host_uuid);	
		printf("Xenserver hostname/UUID internal ERROR!!!!  STOP RUN.[%s][%s]\n", g_host_name, g_host_uuid);	
		exit(-1);		
	}
	
	for	(i = 0;	i <	MAX_EVENT_CNT;	i++) 
	{
		event_msg[i].status			=	'0';
		event_msg[i].event_type		=	0x00;
		memset(event_msg[i].event_time		,	0x00,	sizeof(event_msg[i].event_time));
		memset(event_msg[i].event_code		,	0x00,	sizeof(event_msg[i].event_code));
		memset(event_msg[i].event_msg		,	0x00,	sizeof(event_msg[i].event_msg));
	}

	for	(i = 0;	i <	MAX_MSG_CNT;	i++) 
	{
        backup_info[i] = (BACKUP_INFO *)(shared_memory + sizeof(BACKUP_INFO)*i );
        // ** agent 재기동인 경우 현재 실행중인 task(shared memory)는 내부 오류로 처리해야 함
        // 1(수신), 2(queue 대기)는 유지, 3(처리중)은 오류/확인필요로, 5(처리완료), 9(오류등은 유지)
        // ** 시스템 리부팅인 경우 메모리에는 없는 1,2,3 task가 로그에는 존재하므로 management thread에서 이 부분은 별도 처리 

        if (backup_info[i]->status		==	3)
        {
        	BS_Tsyslog("[%.*s] Task cancel.   Please check result.\n", 17, backup_info[i]->msg_seq);
        	TRAN_LOG2('6', "Task cancel(mode 1)", "%s", &backup_info[i]->msg_type);
        	backup_info[i]->status		=	6;
        }
        
		if (first_run == TRUE)
		{
			backup_info[i]->status		=	0;
			backup_info[i]->fd 			=	0;
			backup_info[i]->msg_len 	=	0;
			backup_info[i]->queue_seq	=	0L;
			memset(&backup_info[i]->msg_type, 0x00,	sizeof(BACKUP_INFO));
		}
	}
		
	// stderr message를 stdout message와 통합 관리
	dup2(2,1);	
	BS_Tsyslog("Environment setup end.\n");
}
int	RealTimeMain()
{
	int				li_Fd, li_Len, i;
	struct 			timeval lt_TimeOut;
	char 			lx_Message[MAX_BUFFER_SIZE];
	int				remain_len;
	char			buffer[MAX_BUFFER_SIZE];
	char			ipaddress[20];
	int				msg_retry;				// length error시 retry counter
	int				val;	
	FD_ZERO(&s_reads);
	FD_SET(si_ServSock,	&s_reads);
	si_FdMax	=	si_ServSock;	


	while(TRUE)
	{
		s_temps				=	s_reads;
		lt_TimeOut.tv_sec	=	WAIT_TIME;
		lt_TimeOut.tv_usec	=	0;

		if (select(si_FdMax	+ 1, &s_temps, 0, 0, &lt_TimeOut) <= 0)
		{
			continue;
		}
		
		for	(li_Fd = 0;	li_Fd <	si_FdMax + 1; li_Fd++)
		{
			if (FD_ISSET(li_Fd,	&s_temps))
			{
				if ((li_Fd == si_ServSock))
				{
					if (BS_NetworkAccept(ipaddress)	== FALSE)
					{
						BS_Tsyslog("Network	accept internal	error.[%d]\n", errno);
						continue;
					}
					FD_SET(si_ClntSock,	&s_reads);
					if (si_FdMax < si_ClntSock)
					{
						si_FdMax = si_ClntSock;
					}
			   
				} 
				else 
				{
					memset(sx_Message, 0x00, sizeof(sx_Message));
					memset(lx_Message, 0x00, sizeof(lx_Message));
					li_Len = read(li_Fd, lx_Message, 7);
					if (li_Len == 0)	/* Close connection	request	*/
					{
						// 메모리 배열에 해당 FD가 있는 경우 fd를 초기화시켜야 다른곳으로 송신되지 않을 것임
						for (i = 0; i < MAX_MSG_CNT; i++)
						{
							if (backup_info[i]->fd == li_Fd)
							{
								BS_Tsyslog("[%d]th fd is closing. [%.*s] Request\n", li_Fd, sizeof(backup_info[i]->msg_seq), backup_info[i]->msg_seq); 
								backup_info[i]->fd 			= 0;
							}
						}						
						FD_CLR(li_Fd, &s_reads);
						close(li_Fd);
//						printf("Close connection request received. [%d]\n",	li_Fd);
						XD_Tsyslog("Close connection request received. [%d]\n",	li_Fd);

					}
					else
					{
						remain_len = atoi(lx_Message);
						li_Len = read(li_Fd, sx_Message, remain_len);
						BS_Tsyslog("Message	recv(from FD=%d) pre=[%d][%d]data=[%s]\n", li_Fd, li_Len, remain_len, sx_Message);
						if (remain_len == 0) {
							BS_Tsyslog("Null stream	data received. skip	this operation\n");
							continue;
						}
						
						total_recv_len = 0;
						if (li_Len != remain_len)
						{
							// length가 맞지 않으면 에러 처리함.  아래 내용 참고해서 보완할 것
							// for (i = 0; i < MAX_MSG_CNT; i++)
							// {
							// 	if (backup_info[i]->fd == li_Fd)
							// 	{
							// 		BS_Tsyslog("[%d]th fd is closing. [%.*s] Request\n", li_Fd, sizeof(backup_info[i]->msg_seq), backup_info[i]->msg_seq); 
							// 		backup_info[i]->fd 			= 0;
							// 	}
							// }
							// FD_CLR(li_Fd, &s_reads);
							// close(li_Fd);
							// XD_Tsyslog("Close connection request received. [%d]\n",	li_Fd);
							// BS_Tsyslog("Message RECV length is not valid[%d][%d][%s]\n", remain_len, li_Len, sx_Message);
							// continue;

							// 수정 필요
							// 만일 수신 길이 오류라면?  알람처리나 카운터 반영하는 것도 좋을 듯
							// 수신된 전문 길이가 맞지 않는 경우 loop 돌리지 않고 에러 처리.  나중에는 아래와 같이 counter를 설정하고 loop 돌릴 것
							
							//*
							msg_retry			=	0;
							BS_Tsyslog("FD = [%d] RECV length is not valid[%d][%d]\n", li_Fd, li_Len, remain_len);
    						if ((val = fcntl(li_Fd, F_GETFL, 0)) < 0) {
    						    BS_Tsyslog("F_GETFL fcntl error: fd[%d] errno[%d]", li_Fd, errno);
								FD_CLR(li_Fd, &s_reads);
								close(li_Fd);
								XD_Tsyslog("Close connection (message length is not valid)[%d]\n",	li_Fd);
								BS_Tsyslog("Close connection (message length is not valid && Internal error). ). FD = [%d]\n",	li_Fd);
								continue;
    						}
    						
    						val |= O_NONBLOCK;
    						
    						if (fcntl(li_Fd, F_SETFL, val) < 0) {
    						    BS_Tsyslog("F_SETFL fcntl error: fd[%d] errno[%d]", li_Fd, errno);
								FD_CLR(li_Fd, &s_reads);
								close(li_Fd);
								XD_Tsyslog("Close connection (message length is not valid)[%d]\n",	li_Fd);
								BS_Tsyslog("Close connection (message length is not valid && Internal error). ). FD = [%d]\n",	li_Fd);
								continue;
    						}
							BS_Tsyslog("FD = [%d] => Non_Blocking mode\n", li_Fd);
    						
							while(remain_len > 0) {
								memcpy(&buffer[total_recv_len],	&sx_Message[0],	li_Len);
								total_recv_len += li_Len;
								remain_len -= li_Len;
														
								// 실제 데이터보다 전문길이 정보가 긴 경우 아래 read에서 non-blocking으로 loop 돌면서 처리
								li_Len = read(li_Fd, sx_Message, atoi(lx_Message));
								if (li_Len == -1)
								{
									if (++msg_retry > MAX_MSG_RETRY)
									{
										FD_CLR(li_Fd, &s_reads);
										close(li_Fd);
										XD_Tsyslog("Close connection (message length is not valid). [%d]\n",	li_Fd);
										BS_Tsyslog("Close connection (message length is not valid). FD = [%d]\n",	li_Fd);
										for (i = 0; i < MAX_MSG_CNT; i++)
										{
											if (backup_info[i]->fd == li_Fd)
											{
												BS_Tsyslog("[%d]th fd is closing. [%.*s] Request\n", li_Fd, sizeof(backup_info[i]->msg_seq), backup_info[i]->msg_seq); 
												backup_info[i]->fd 			= 0;
											}
										}
										break;
									}
									li_Len =	0;
								}
								sleep(1);
								BS_Tsyslog("RECV retry=[%d]th: Message recv total=[%d][%d]real=[%d]data=[%s]\n", msg_retry, atoi(lx_Message),	total_recv_len,	li_Len,	sx_Message);
							}
							// FD close 한 경우
							if (msg_retry > MAX_MSG_RETRY)
							{
								continue;
							}
							// loop count가 0보다 큰 경우이면서 정상 처리된 경우 fd를 block mode로 바꿈
							else if (msg_retry > 0)
							{
    							if ((val = fcntl(li_Fd, F_GETFL, 0)) < 0) {
    							    BS_Tsyslog("F_GETFL fcntl error: fd[%d] errno[%d]", li_Fd, errno);
									FD_CLR(li_Fd, &s_reads);
									close(li_Fd);
									XD_Tsyslog("Close connection (message length is not valid)[%d]\n",	li_Fd);
									BS_Tsyslog("Close connection (message length is not valid && Internal error #2). FD = [%d]\n",	li_Fd);
									continue;
    							}
    							
    							val &= ~O_NONBLOCK;
    							
    							if (fcntl(li_Fd, F_SETFL, val) < 0) {
    							    BS_Tsyslog("F_SETFL fcntl error: fd[%d] errno[%d]", li_Fd, errno);
									FD_CLR(li_Fd, &s_reads);
									close(li_Fd);
									XD_Tsyslog("Close connection (message length is not valid)[%d]\n",	li_Fd);
									BS_Tsyslog("Close connection (message length is not valid && Internal error #2). FD = [%d]\n",	li_Fd);
									continue;
    							}
								BS_Tsyslog("FD = [%d] => Blocking mode\n", li_Fd);
							}
							memcpy(&sx_Message[0], &buffer[0], total_recv_len);
							BS_Tsyslog("Message	recv final=[%d][%d]real=[%d]data=[%s]\n", atoi(lx_Message),	total_recv_len,	li_Len,	sx_Message);
							//*/
						}
						si_CurrFd =	li_Fd;		// 현재	fd 정보를 전역변수에 저장
						total_recv_len = atoi(lx_Message);
						
						if (MessageProcessing()	== FALSE)
						{
							BS_Tsyslog("Processing Faied. Received data	is [%s]\n",	sx_Message);
						}
						else
						{
							// BS_Tsyslog("Processing finished. Received data is [%s]\n", sx_Message);
						}
					} /* if	(li_Len	...	*/
				} /* if	((li_Fd	...	*/

			} /* if	(FD_IS ... */
		} /* for (li_Fd	...	*/
	} /* While loop	*/
}

static int MessageProcessing()
{
	int				result;
	int				i;
	int				send_len;
	char			buffer[MAX_BUFFER_SIZE];
	char			curr_priority[20];

	if (sx_Message[0] == 'B')		// Backup message (for samsung SDS)
	{
//		BS_Tsyslog("Backup Message received.\n");
		
		// 요청 task 처리 등록
		memset(buffer, 0x00, sizeof(buffer));
		memset(curr_priority, 0x00, sizeof(curr_priority));
		sprintf(buffer, "%07d", total_recv_len);
		memcpy(&buffer[7], &sx_Message[0], total_recv_len);
		
		for (i = 0; i < MAX_MSG_CNT; i++)
		{
			if ((backup_info[i]->status == 0) || (backup_info[i]->status > 3)) // 0 : 미사용, 8 : Queue full error, 9: Error
			{
				backup_info[i]->fd 			= si_CurrFd;							// 현재 처리되고 있는 socket session fd 저장
				backup_info[i]->msg_len		= total_recv_len;						// 전문 전체 길이 (queue 처리를 위해)
				memset(backup_info[i]->msg_text, 0x00, sizeof(backup_info[i]->msg_text));
				memcpy(&backup_info[i]->msg_type, &sx_Message[0], total_recv_len);	// 전체 전문 내용 저장
				backup_info[i]->status 		= 1;
				sequence_handler(0);												// unique한 queue id 증가
				sprintf(curr_priority, "%c", backup_info[i]->priority);
				// unique한 queue id를 생성하기 위해 priority * 1억 + current seq를 계산하여 queue제 저장
				backup_info[i]->queue_seq	= atol(curr_priority) * 100000000L + current_queue_seq;
//				BS_Tsyslog("Current queue seq[%ld], [%s], [%d]\n", 	backup_info[i]->queue_seq, curr_priority, current_queue_seq);
//				BS_Tsyslog("Backup Message 등록(In memory)[%d]th saved. [%25s]\n", i, sx_Message);
		        TRAN_LOG2('1', "Message Receive", "%s", &buffer[7]);
				return TRUE;
			}
		}
		
		if (i >= MAX_MSG_CNT)
		{
			BS_Tsyslog("현재 처리 대기중인 task 최대치[%d] 초과.\n", i);
			BS_Tsyslog("task 오류 처리.[%s]\n", sx_Message);
			// struct 구조가 바뀌면 수정할 부분
			buffer[18+7] = '8';									// 실행 대기 task full 

	        TRAN_LOG2('8', "Queue full", "%s", &buffer[7]);

			send_len = write(si_CurrFd, buffer, total_recv_len + 7);
		    if (send_len != (total_recv_len + 7))
		    {
		        BS_Tsyslog("송신 오류 : Backup manager. internal error[%d][%d]\n", send_len, total_recv_len + 7);
		    }
		    
		    // 
			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				BS_Tsyslog("TASK DEBUG [%d]=>[%d][%s]\n", i, backup_info[i]->status, backup_info[i]->msg_text);		
			}
			return FALSE;	
		}		
	}

	// 이하 format error 처리		
	BS_Tsyslog("request mesage format error.\n");
	BS_Tsyslog("task 오류 처리.[%30s]\n", sx_Message);
	sprintf(buffer, "%07d", total_recv_len);
	memcpy(&buffer[7], &sx_Message[0], total_recv_len);
	// struct 구조가 바뀌면 수정할 부분
	buffer[18+7] = '9';									// format 오류 error 처리 
	TRAN_LOG2('9', "Format Error", "%s", &buffer[7]);
	send_len = write(si_CurrFd, buffer, total_recv_len + 7);

	if (send_len != (total_recv_len + 7))
	{
	    BS_Tsyslog("Send to Requester manager. format error[%d][%d]\n", send_len, total_recv_len + 7);
	}
	return FALSE;
}

void *health_check_processing(void *args)
{
	char	buffer[MAX_BUFFER_SIZE];
	char	command[100], disk_usage[10];
	int		interval;						// 스토리지 모니터링 주기
	int		retry;							// 재시도 횟수 MAX
	int		curr_retry    = 0;				// 현재 재시도 횟수
	char	mount_point[100];				// 스토리지 mount point	
	int		storage_monitor	=	TRUE;		//스토리지 모니터 flag
	char	total_mem[15];
	char	usage_mem[15];
	char	cpu_usage_result[10];
	char	dummy[1000];
	int		i;

	DUMMY_MSG	dummy_msg;

	while(TRUE)
	{
		memset(buffer,	0x00,	sizeof(buffer));
		sprintf(buffer, "%s", "df -Pk /|tail -1");
		dummy_call(health_value, HEALTH_CHECK_INTERVAL, buffer);
		
		if (memcmp(buffer, "/dev", 4) != 0)
		{
			BS_Tsyslog("Check / disk failed.[%s]\n", buffer);
		}
		else
		{
			sscanf(buffer, "%s %s %s %s %s", command, command, command, command, disk_usage);		
//			BS_Tsyslog("Disk status.[%s][%d]\n", buffer, atoi(disk_usage));
			health_check.disk_usage	=	atoi(disk_usage);		
		}

		memset(buffer,	0x00,	sizeof(buffer));
		sprintf(buffer, "%s", "xe host-list");
//BS_Tsyslog("xe host-list before [%s]\n", buffer);

		dummy_call(health_value, HEALTH_CHECK_INTERVAL, buffer);
//BS_Tsyslog("xe host-list after [%s]\n", buffer);

		if (memcmp(buffer, "uuid", 4) != 0)
		{
			strcpy(health_check.xenserver, "Fail");
//BS_Tsyslog("event_register before [%s]\n", buffer);		
			event_register('C', "XEN000999", "Xenserver health check status is Failed.");
//BS_Tsyslog("event_register after [%s]\n", buffer);
//BS_Tsyslog("Check xenserver status. Fail[%s]\n", buffer);
		}
		else
		{
			strcpy(health_check.xenserver, "Active");
//BS_Tsyslog("Xenserver status.[%s]\n", buffer);
		}

		// CPU 사용율 check
		memset(buffer,	0x00,	sizeof(buffer));
//		sprintf(buffer, "%s", "sar 3 1|grep Average|awk '{print $8}'");
		sprintf(buffer, "%s", "sar 3 1|grep Average");
		dummy_call(health_value, HEALTH_CHECK_INTERVAL, buffer);
		memset(cpu_usage_result, 0x00, sizeof(cpu_usage_result));
		sscanf(buffer, "%s %s %s %s %s %s %s %s", dummy, dummy, dummy, dummy, dummy, dummy, dummy, cpu_usage_result);
		health_check.cpu_usage	=	100 - atof(cpu_usage_result);
//BS_Tsyslog("Current cpu status = [%s], %f\n", buffer, health_check.cpu_usage);

		// MEM 사용율 check
		memset(buffer,	0x00,	sizeof(buffer));
//		sprintf(buffer, "%s", "free|grep Mem|awk '{print $2,$3}'");
		sprintf(buffer, "%s", "free|grep Mem");
		dummy_call(health_value, HEALTH_CHECK_INTERVAL, buffer);

		sscanf(buffer, "%s %s %s", dummy, total_mem, usage_mem);
		health_check.mem_usage	=	100 * atof(usage_mem) / atof(total_mem);
//BS_Tsyslog("Current memory usage = [%s][%s], %f\n", total_mem, usage_mem, health_check.mem_usage);
				
		// 스토리지 모니터링
		if (BS_GetProfileString("VBR Agent for Xenserver", "Interval Time", buffer, sizeof(buffer)) == FALSE)
    	{
        	BS_Tsyslog("FATAL Error. Config file[%s] [%s] storage monitor Interval Time parameter not found...\n", CONFIG_FILE, "Interval Time");
    		storage_monitor	=	FALSE;    		
    	}
    	else
    	{
    		interval = atoi(buffer);
    	}
		if (BS_GetProfileString("VBR Agent for Xenserver", "Retry Count", buffer, sizeof(buffer)) == FALSE)
    	{
        	BS_Tsyslog("FATAL Error. Config file[%s] [%s] storage monitor Retry Count parameter not found...\n", CONFIG_FILE, "Retry Count");
    		storage_monitor	=	FALSE;    		
    	}
    	else
    	{
    		retry = atoi(buffer);    		
    	}
    	
		if (BS_GetProfileString("VBR Agent for Xenserver", "Mount Point", buffer, sizeof(buffer)) == FALSE)
    	{
        	BS_Tsyslog("FATAL Error. Config file[%s] [%s] storage monitor Mount Point parameter not found...\n", CONFIG_FILE, "Mount Point");
    		storage_monitor	=	FALSE;    		
    	}
    	else
    	{
    		strcpy(mount_point, buffer);
    	}

		if (BS_GetProfileString("VBR Agent for Xenserver", "CPU Limit", buffer, sizeof(buffer)) == FALSE)
    	{
        	BS_Tsyslog("FATAL Error. Config file[%s] [%s] CPU Limit parameter not found...\n", CONFIG_FILE, "CPU Limit");
    		storage_monitor	=	FALSE;    		
    	}
    	else
    	{
    		cpu_limit	=	atof(buffer);
    	}
    	
    	// 스토리지 모니터링 환경 및 status가 정상일때 mount point check
    	if (storage_monitor)
    	{
			memset(buffer,	0x00,	sizeof(buffer));
			sprintf(buffer, "touch %s/ALIVE_%s.chk", mount_point, g_host_name);
			dummy_call(health_value, HEALTH_CHECK_INTERVAL, buffer);
			if (strlen(buffer) != 0)
			{
				if (curr_retry <= retry)
				{
					curr_retry 	+=	1;
					BS_Tsyslog("Check storage cloud [%d]th error.\n", curr_retry);
				}
				if ((curr_retry > retry) && (health_check.storage[0] == 'A' || health_check.storage[0] == '#'))
				{
					strcpy(health_check.storage, "Fail");
					event_register('C', "STG000999", "Storage cloud Status monitoring Failed.");
					BS_Tsyslog("Storage cloud Status is Failed.  reason=[%s]\n", buffer);
					cancel_async_task();
				}
			}
			else
			{
				strcpy(health_check.storage, "Active");
				if (curr_retry > 0)
				{
					BS_Tsyslog("Storage cloud Status is OK!!!\n");					
				}
				curr_retry 	=	0;
//BS_Tsyslog("Xenserver status.[%s]\n", buffer);
			}
		}
		else
		{
			curr_retry 	+=	1;
//BS_Tsyslog("Check storage cloud [%d]th error.[%s]\n", curr_retry, buffer);
			if ((curr_retry > retry) && (health_check.storage[0] == 'A' || health_check.storage[0] == '#'))
			{
				strcpy(health_check.storage, "Fail");
				event_register('C', "STG000999", "Storage cloud Status monitoring Failed.");
				BS_Tsyslog("Check storage cloud [%d]th error.[%s]\n", curr_retry, buffer);
			}
    	}
    	
		for (i = 0; i < MAX_MSG_CNT; i++)
		{
			if (((backup_info[i]->status >= 1) && (backup_info[i]->status <= 3)) && (strlen(backup_info[i]->msg_text) == 0)) // status 값은 처리중이나 실제 내부 오류인 경우
			{
				backup_info[i]->status = 0;
				BS_Tsyslog("TASK [%d]th status change to [%d]\n", i, backup_info[i]->status);
			}
		}
    	storage_monitor	=	TRUE;
		BS_date_time(health_check.last_time);
	}
}

void *backup_receiver(void *args)
{
	Msg				msg;
	struct			msqid_ds	ds;
	int				sync_queue_id;
	int				async_queue_id;
	int				query_queue_id;
	int				queue_id;
	int				result;
	int				i;
	char 			temp[10];
	int				send_len;
	char			buffer[MAX_BUFFER_SIZE];
	

	sync_queue_id 			=	msgget(sync_msg_key, IPC_CREAT | 0644);
	async_queue_id 			=	msgget(async_msg_key, IPC_CREAT | 0644);
	query_queue_id 			=	msgget(mgt_msg_key, IPC_CREAT | 0644);

	BS_Tsyslog("Backup thread (Receiver) start...\n");
		
	while (TRUE)
	{
		for (i = 0; i < MAX_MSG_CNT; i++)
		{
			if (backup_info[i]->status == 1)							// 미처리 전문
			{
				// DEBUG
				// sprintf(temp, "%c%c", backup_info[i]->priority, 0x00);
				// memset(&msg, 0x00, sizeof(msg));
				// msg.msg_type		=	atol(temp);
				memset(&msg, 0x00, sizeof(msg));
				msg.msg_type		=	backup_info[i]->queue_seq;
				memcpy(msg.msg_text, &backup_info[i]->msg_type, backup_info[i]->msg_len);
//				BS_Tsyslog("TYPE : %ld, [%s]\n", msg.msg_type, msg.msg_text);
				switch (backup_info[i]->task_type)
				{
					case 'S' : queue_id 		=	sync_queue_id;
						       break;
					case 'A' : queue_id 		=	async_queue_id;
						       break;
					case 'Q' :
					case 'C' :
					case 'T' :
					case 'I' :						 
							   queue_id 		=	query_queue_id;
						       break;
					default  : BS_Tsyslog("TASK type format error\n");
							   sprintf(buffer, "%07d", backup_info[i]->msg_len);
							   memcpy(&buffer[7], backup_info[i]->msg_text, backup_info[i]->msg_len);
							   // struct 구조가 바뀌면 수정할 부분
							   buffer[18+7] = '9';									// format 오류 error 처리 
							   TRAN_LOG2('9', "TASK Format Error", "%s", &buffer[7]);
							   send_len = write(backup_info[i]->fd, buffer, backup_info[i]->msg_len + 7);
    						   
							   if (send_len != (backup_info[i]->msg_len + 7))
							   {
							       BS_Tsyslog("Send to Requester manager. format error[%d][%d]\n", send_len, total_recv_len + 7);
							   }
							   continue;
				}
				backup_info[i]->status = 2;		

				TRAN_LOG2('2', "Queue saving", "%s", msg.msg_text);       						
				
				result				=	msgsnd(queue_id, &msg, backup_info[i]->msg_len, 0);
				if (result == -1)
				{
					BS_Tsyslog("Queue id %d, send ERROR %d\n", queue_id, errno);
					backup_info[i]->status = 9;						// queue insert error 처리, 추가 보완해 둘 것
				}
				else
				{
					backup_info[i]->status = 2;						// queue insert 완료					
//		    		BS_Tsyslog("Queue saved : %s\n", msg.msg_text);
				}
				
				if (backup_info[i]->task_type == 'A')
				{
					memset(buffer, 0x00, sizeof(buffer));
					sprintf(buffer, "%07d", 25);
					// struct 구조가 바뀌면 수정할 부분
					memcpy(&buffer[7], &sx_Message[0], 23);
					strcpy(&buffer[23+7], "OK");
					buffer[18+7] = '0';
					send_len = write(backup_info[i]->fd, buffer, strlen(buffer));
            		
		    		if (send_len != strlen(buffer))
		    		{
		    		    BS_Tsyslog("송신 오류 : Backup manager. internal error[%d][%d][%s]\n", send_len, total_recv_len + 7, buffer);
//				        TRAN_LOG2('9', "ASYNC reply error", "%s", &buffer[7]);
				        continue;
		    		}
		    		else
		    		{
//						BS_Tsyslog("ASYNC AUTO reply OK", "%s", &buffer[7]);									// 실행 대기 && return true
		    		}
				}				
			}
				
		} // for
		sleep(1);
		// BS_Tsyslog("DEBUG : Queue search finished...\n");		
	} // while
	
}

void *backup_sync_sender(void *args)
{
	Msg				msg;
	struct			msqid_ds	ds;
	key_t			sync_msg_key		=	20001;				// 임시정의 (Sync)
	int				sync_queue_id;
	int				result;
	int				i;
	char 			temp[10];
	BACKUP_INFO		sync_info;
	int				send_len;
	ssize_t			nbytes;
	char			buffer[MAX_SEND_SIZE];
	
	sync_queue_id 			=	msgget(sync_msg_key, IPC_CREAT | 0644);

	BS_Tsyslog("Backup thread (SYNC) start...\n");

		
	while (TRUE)
	{
		memset(&msg, 0x00, sizeof(msg));
		nbytes 					=	msgrcv(sync_queue_id, &msg, MAX_DATA_SIZE, 0, 0);
		
		if (nbytes > 0)
		{
//			BS_Tsyslog("SYNC Thread : RECV msg is : len = [%d]  queue-type = [%ld] data = [%s]\n", nbytes, msg.msg_type, msg.msg_text);
			memcpy(&sync_info.msg_type, msg.msg_text, nbytes);

			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				if (memcmp(backup_info[i]->msg_seq, sync_info.msg_seq, sizeof(sync_info.msg_seq)) == 0)							// 요청 전문 검색, 완료 처리, 응답 전문 송신등...
				{
					memset(buffer, 0x00, sizeof(buffer));
					sprintf(buffer, "%07d", nbytes);
					memcpy(&buffer[7], msg.msg_text, nbytes);
					send_len = write(backup_info[i]->fd, buffer, nbytes+7);
					if (send_len != nbytes+7)
   					{
						BS_Tsyslog("Send to REQUEST Manager internal error[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "SYNC RESULT send error", "%s", &buffer[7]);
						backup_info[i]->status 		=	9;
					}
					else
					{
//						BS_Tsyslog("Send to REQUEST Manager 송신 완료[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "SYNC RESULT send success", "%s", &buffer[7]);
						memset(&backup_info[i]->status, 0x00, sizeof(sync_info));
						backup_info[i]->status 		=	0;
					}
					break;
				}
			} // for
		}
		else
		{
			if (errno == ENOMSG)
			{
				BS_Tsyslog("Sync Queue is empty.\n");
				continue;
			}
			else
			{
				BS_Tsyslog("Sync Internal ERROR [%d].\n", errno);
				continue;
			}
		}
		
		if (i >= MAX_MSG_CNT)
		{
			BS_Tsyslog("원본 요청 전문 검색 에러[%s]\n", msg.msg_text);
		}
	}	
}

void *monitor(void *args)			// VBR agent 처리현황 display
{
	int		i;
	int		sync[2], async[2], mgt[2];		// [0] : Queue 대기, [1] : 처리중
	struct	timeval	current_time;
	// 장학재단용 모니터 OFF
	return;
	printf("%c[H%c[2J", 27, 27);
	printf("Environment status checking...... (5 SEC WAIT)");
	sleep(5);

	// initialize를 위해 wait
	
	while (TRUE)
	{	
		sync[0] = 0; 	sync[1] = 0;
		async[0] = 0; 	async[1] = 0;
		mgt[0] = 0; 	mgt[1] = 0;
		printf("%c[H%c[2J", 27, 27);

		gettimeofday(&current_time, NULL);

		for (i = 0; i < MAX_MSG_CNT; i++)
		{
//			printf("DEBUG : [%d][%d][%c][%s]\n", i, backup_info[i]->status, backup_info[i]->task_type, backup_info[i]->msg_seq);
			if ((backup_info[i]->status != 1) && (backup_info[i]->status != 2) && (backup_info[i]->status != 3))	// 0 : 미사용, 8 : Queue full error, 9: Error
			{
				continue;
			}
			if ((backup_info[i]->status == 1) || (backup_info[i]->status == 2))			// Queue 대기
			{
// printf("queue 대기 [%d][%c]\n", backup_info[i]->status, backup_info[i]->task_type); 
				switch (backup_info[i]->task_type)
				{
						case 'S' : sync[0]++;
							       break;
						case 'A' : async[0]++;
							       break;
						case 'Q' :
						case 'C' :
						case 'T' :
						case 'I' :
								   mgt[0]++;
							       break;
						default	 : BS_Tsyslog("MONITOR mode change (1/2) [%c]\n", backup_info[i]->task_type);
								   // internal error 처리			
							       backup_info[i]->status = 9;
				}
			}
    	
			if (backup_info[i]->status == 3)											// 처리중
			{
// printf("실행중 [%d][%c]\n", backup_info[i]->status, backup_info[i]->task_type); 
				switch (backup_info[i]->task_type)
				{
						case 'S' : sync[1]++;
							       break;
						case 'A' : async[1]++;
							       break;
						case 'Q' :
						case 'C' :
						case 'T' :
						case 'I' :
								   mgt[1]++;
							       break;
						default	 : BS_Tsyslog("MONITOR mode change (3) [%c]\n", backup_info[i]->task_type);
								   // internal error 처리			
							       backup_info[i]->status = 9;			
				}
			}
		}
// printf("sum [%d][%d][%d][%d][%d][%d]\n", sync[0], sync[1], async[0], async[1], mgt[0], mgt[1]); 
		printf("=== Health check status ===[%s]\n", current_version);
		printf("[%s] Xenserver : %s, Storage Cloud : %s, Dom0 CPU usage = %5.2f%(%5.2f%), Disk usage = %d%, Memory usage = %5.2f%\n", health_check.last_time, health_check.xenserver, health_check.storage, health_check.cpu_usage, cpu_limit, health_check.disk_usage, health_check.mem_usage);
		printf("=== Sync  thread status ===(Queue wait : %d / processing : %d )\n", sync[0], 	sync[1]);
		for (i = 0; i < MAX_SYNC_THREAD_CNT; i++)
		{
			if (sync_thread[i].run_flag[0] == 'A')
			{
	    		if (current_time.tv_usec - sync_thread[i].start_time.tv_usec < 0)
	    		{
	    		    current_time.tv_sec--;
	    		    current_time.tv_usec += 1000000;
	    		}	
	    		sync_thread[i].sec			=	(int)  current_time.tv_sec  - sync_thread[i].start_time.tv_sec;
        		sync_thread[i].msec			=	(long) current_time.tv_usec - sync_thread[i].start_time.tv_usec;	
				printf("[%d]:[%s][%s][%d.%06ld][%s]\n", i+1, sync_thread[i].run_flag, sync_thread[i].last_time, sync_thread[i].sec, sync_thread[i].msec, sync_thread[i].msg_text);				
			}
			else if (sync_thread[i].run_flag[0] == 'S')
			{ 
				printf("[%d]:[%s][%s][%d.%06ld][%s]\n", i+1, sync_thread[i].run_flag, sync_thread[i].last_time, sync_thread[i].sec, sync_thread[i].msec, sync_thread[i].msg_text);
			}
		}
		printf("=== Async thread status ===(Queue wait : %d / processing : %d )\n", async[0], 	async[1]);
		for (i = 0; i < MAX_ASYNC_THREAD_CNT; i++)
		{
			if (async_thread[i].run_flag[0] == 'A')
			{
	    		if (current_time.tv_usec - async_thread[i].start_time.tv_usec < 0)
	    		{
	    		    current_time.tv_sec--;
	    		    current_time.tv_usec += 1000000;
	    		}	
	    		async_thread[i].sec			=	(int)  current_time.tv_sec  - async_thread[i].start_time.tv_sec;
        		async_thread[i].msec		=	(long) current_time.tv_usec - async_thread[i].start_time.tv_usec;	
				printf("[%d]:[%s][%s][%d.%06ld][%.17s][%s]\n", i+1, async_thread[i].run_flag, async_thread[i].last_time, async_thread[i].sec, async_thread[i].msec, async_thread[i].msg_seq, async_thread[i].msg_text);				
			}
			else if (async_thread[i].run_flag[0] == 'S')
			{ 
				printf("[%d]:[%s][%s][%d.%06ld][%.17s][%s]\n", i+1, async_thread[i].run_flag, async_thread[i].last_time, async_thread[i].sec, async_thread[i].msec, async_thread[i].msg_seq, async_thread[i].msg_text);
			}
			else if (async_thread[i].run_flag[0] == 'P')
			{ 
				printf("[%d]:[%s][%s][%d.%06ld][%.17s][%s]\n", i+1, async_thread[i].run_flag, async_thread[i].last_time, async_thread[i].sec, async_thread[i].msec, async_thread[i].msg_seq, async_thread[i].msg_text);
			}
		}
		printf("=== MGMT  thread status ===(Queue wait : %d / processing : %d )\n", mgt[0], 	mgt[1]);
		for (i = 0; i < MAX_MGT_THREAD_CNT; i++)
		{
			if (mgt_thread[i].run_flag[0] == 'A')
			{
	    		if (current_time.tv_usec - mgt_thread[i].start_time.tv_usec < 0)
	    		{
	    		    current_time.tv_sec--;
	    		    current_time.tv_usec += 1000000;
	    		}	
	    		mgt_thread[i].sec			=	(int)  current_time.tv_sec  - mgt_thread[i].start_time.tv_sec;
        		mgt_thread[i].msec			=	(long) current_time.tv_usec - mgt_thread[i].start_time.tv_usec;	
				printf("[%d]:[%s][%s][%d.%06ld][%s]\n", i+1, mgt_thread[i].run_flag, mgt_thread[i].last_time, mgt_thread[i].sec, mgt_thread[i].msec, mgt_thread[i].msg_text);				
			}
			else if (mgt_thread[i].run_flag[0] == 'S')
			{ 
				printf("[%d]:[%s][%s][%d.%06ld][%s]\n", i+1, mgt_thread[i].run_flag, mgt_thread[i].last_time, mgt_thread[i].sec, mgt_thread[i].msec, mgt_thread[i].msg_text);
			}
		}
		sleep(3);		
	}
}

void *mgt_processing(void *args)		// Query/Cancel/Thread/Inquire type 전문 처리
{
	int				recv_queue_id;
	int				send_queue_id;
	int				msg_size;
	int				result;
//	ssize_t			nbytes;
	long			nbytes;
	BACKUP_INFO		msg_info;
	char			buffer[MAX_BUFFER_SIZE];
	FILE			*fp;
	int				find;					// log key를 찾은 경우 flag.  0 : false, 1: true
	int				thread_seq;
	Msg				msg;
	int				stime, etime;			// 시작/종료시간
	struct timeval  StartTime, EndTime;
	int				i;
	int				index;
	long			send_len;

	recv_queue_id			=	msgget(mgt_recv_msg_key, IPC_CREAT | 0644);
	send_queue_id			=	msgget(mgt_send_msg_key, IPC_CREAT | 0644);

	thread_seq				=	*((int *)args);
//	thread_seq				=	(int) (int *)args;
	BS_Tsyslog("VBR Agent Management thread[%d]th start.\n", thread_seq);

	find	=	0;
	while (TRUE)
	{
//		nbytes 					=	msgrcv(queue_id, &msg, MAX_DATA_SIZE, 0, IPC_NOWAIT);
		memset(&msg, 0x00, sizeof(msg));
		memset(buffer, 0x00, sizeof(buffer));
		memset(&msg_info, 0x00, sizeof(msg_info));
		nbytes 					=	msgrcv(recv_queue_id, &msg, MAX_DATA_SIZE, -2100000000L, 0);
//		gettimeofday(&StartTime, NULL);
		gettimeofday(&mgt_thread[thread_seq].start_time, NULL);
//		BS_Tsyslog("MGT : RECV msg : len = [%d]  queue-type = [%ld] data = [%s]\n", nbytes, msg.msg_type, msg.msg_text);
		
		if (nbytes > 0)
		{
			// thread kill msg이면 thread 종료
			if ((msg.msg_type	==	1L) && (memcmp(msg.msg_text, "KILL", strlen("KILL")) == 0))
			{
				break;
			}

			TRAN_LOG2('3', "Management Queue receive", "%s", msg.msg_text);
			memcpy(&msg_info.msg_type, msg.msg_text, nbytes);

			// Status 처리중으로 변경
			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				if (memcmp(backup_info[i]->msg_seq, msg_info.msg_seq, sizeof(msg_info.msg_seq)) == 0)
				{
					backup_info[i]->status 	=	3;	
					break;
				}
			}
			if (i == MAX_MSG_CNT)
			{
				BS_Tsyslog("************  원본 전문 검색 오류[%s]\n", 	msg_info.msg_seq);
			}

//	BS_Tsyslog("VBR Agent Management thread[%d]th 1.\n", thread_seq);
			// thread information update
			strcpy(mgt_thread[thread_seq].run_flag, "Active");
			sprintf(mgt_thread[thread_seq].msg_text, "%s", msg_info.msg_text);
			BS_date_time(mgt_thread[thread_seq].last_time);
			BS_date_time_no_delimiter2(mgt_thread[thread_seq].last_time2);
//	BS_Tsyslog("VBR Agent Management thread[%d]th 2.\n", thread_seq);

			// TASK 명령인 경우는 별도 처리
			switch (msg_info.task_type)
			{
				case 	'I' :
//								BS_Tsyslog("Before INQ.\n");
								result	=	inquire_task(thread_seq, &msg_info);
//								BS_Tsyslog("AFTER INQ.\n");
								break;
				case 	'C' :
								result	=	cancel_task(thread_seq, &msg_info);
								break;
				case 	'Q' :
								result	=	query_task(thread_seq, &msg_info);
								break;
				case 	'T' :
								result	=	thread_task(thread_seq, &msg_info);
								break;
				default		:	BS_Tsyslog("TASK type is not valid.[%c]\n", msg_info.task_type);
								result	=	FALSE;
								strcpy(msg_info.msg_text, "TASK type is not valid");
								break;
			}
//	BS_Tsyslog("VBR Agent Management thread[%d]th 3.\n", thread_seq);
								
			if (result	==	FALSE)
			{
				msg_info.task_type	=	'9';
			}
//			BS_Tsyslog("AAAA\n");
			nbytes = 1+17+1+3+1+strlen(msg_info.msg_text);
//			BS_Tsyslog("AAAA[%ld]\n", nbytes);
			memset(msg.msg_text, 0x00, sizeof(msg.msg_text));			
//			BS_Tsyslog("BBBB\n");
			memcpy(msg.msg_text, &msg_info.msg_type, nbytes);
//SK

			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				if (memcmp(backup_info[i]->msg_seq, msg_info.msg_seq, sizeof(msg_info.msg_seq)) == 0)							// 요청 전문 검색, 완료 처리, 응답 전문 송신등...
				{
//					memset(buffer, 0x00, sizeof(buffer));
//					sprintf(buffer, "%07d", nbytes);
//					memcpy(&buffer[7], msg.msg_text, nbytes);
//					send_len = write(backup_info[i]->fd, buffer, nbytes+7);
//					len = strlen(sync_info.msg_text);
//					sprintf(buffer, "%07d", len);
//					memcpy(&buffer[7], sync_info.msg_text, len);
//					send_len = write(backup_info[i]->fd, buffer, len+7);
					memset(buffer, 0x00, sizeof(buffer));
					sprintf(buffer, "%07d", nbytes);
					memcpy(&buffer[7], msg.msg_text, nbytes);
					send_len = write(backup_info[i]->fd, buffer, nbytes+7);


					if (send_len != nbytes+7)
   					{
						BS_Tsyslog("Send to REQUEST Manager internal error[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "QUERY RESULT send error", "%s", &buffer[7]);
						backup_info[i]->status 		=	9;
					}
					else
					{
//						BS_Tsyslog("Send to REQUEST Manager 송신 완료[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "QUERY RESULT send success", "%s", &buffer[7]);
						memset(&backup_info[i]->status, 0x00, sizeof(msg_info));
						backup_info[i]->status 		=	0;
					}
					break;
				}
			} // for
			if (i >= MAX_MSG_CNT)
			{
				BS_Tsyslog("원본 요청 전문 검색 에러[%s]\n", msg.msg_text);
			}
		
			// BS_Tsyslog("CCCC[%ld][%ld]\n", strlen(msg.msg_text), strlen(msg_info.msg_text));
			// 
			// result				=	msgsnd(send_queue_id, &msg, nbytes, 0);
			// BS_Tsyslog("DDDD[%d]\n", result);
			// if (result == -1)
			// {
			// 	BS_Tsyslog("QUERY Queue id %d, send ERROR %d\n", send_queue_id, errno);
			// 	TRAN_LOG2('5', "QUERY Queue result send error", "%s", msg.msg_text);
			// }
			// else
			// {
			// 	BS_Tsyslog("QUERY Queue id %d, send SUCCESS msg ==> [%s]\n", send_queue_id, msg.msg_text);
			// 	TRAN_LOG2('5', "QUERY Queue result send finished", "%s", msg.msg_text);
			// }
		}
		else
		{
			if (errno == ENOMSG)
			{
				BS_Tsyslog("MGT Queue is empty.\n");
				continue;
			}
			else
			{
				BS_Tsyslog("MGT Internal ERROR [%d][%s].\n", errno, strerror(errno));
				continue;
			}
		}
		// Status 처리완료
		backup_info[i]->status 	=	5;	
		gettimeofday(&EndTime, NULL);
	    if (EndTime.tv_usec - mgt_thread[thread_seq].start_time.tv_usec < 0)
	    {
	        EndTime.tv_sec--;
	        EndTime.tv_usec += 1000000;
	    }	
	    mgt_thread[thread_seq].sec		=	(int)  EndTime.tv_sec  - mgt_thread[thread_seq].start_time.tv_sec;
        mgt_thread[thread_seq].msec		=	(long) EndTime.tv_usec - mgt_thread[thread_seq].start_time.tv_usec;	
		strcpy(mgt_thread[thread_seq].run_flag, 	"Standby");
		BS_Tsyslog("Management [%d]th thread process. finished\n", thread_seq);
	}	
	mgt_thread[thread_seq].run_flag[0]  =  'N';
	BS_Tsyslog("Management [%d]th thread process is stop.\n", thread_seq);	
}

int	inquire_task(int thd_seq, BACKUP_INFO *backup_msg)
{
	int			i, j;
	char		temp[MAX_BUFFER_SIZE];
	char		buffer[MAX_BUFFER_SIZE];
	char		command[MAX_BUFFER_SIZE];
	char		buffer2[MAX_BUFFER_SIZE];
	char		command2[MAX_BUFFER_SIZE];
	int			sync[2], async[2], mgt[2];		// [0] : Queue 대기, [1] : 처리중
	int			first;
	int			result;
	int			li_timeout;
	char		pool_uuid[100];
	char		pool_name_label[100];	
	FILE		*fp, *fp2;
	char		pool_uuid_text[100] 		= "uuid ( RO)          : ";
	char		pool_name_label_text[100]	= "    name-label ( RW): ";
	char		host_uuid_text[100] 		= "uuid ( RO)          : ";
	char		host_name_label_text[100] 	= "    name-label ( RW): ";
	char		host_address_text[100]		= "       address ( RO): ";
	char		host_uuid[100];
	char		host_name_label[100];
	char		host_ip[100];
	char		vm_uuid_text[100] 			= "uuid ( RO)           : ";
	char		vm_name_label_text[100] 	= "     name-label ( RW): ";
	char		power_state_text[100]		= "    power-state ( RO): ";
	char		resident_on_text[100]		= "    resident-on ( RO): ";
	char		os_version_text[100]		= "     os-version (MRO): ";
	char		networks_text[100]			= "       networks (MRO): ";
	char		vm_uuid[100];
	char		vm_name_label[100];
	char		power_state[100];
	char		resident_on[100];
//	char		resident_xenserver[100];
	char		os_version[100];
	char		networks[100];
	int			counter;
	char		sr_pool_uuid_text[100] 		= "uuid ( RO)          : ";
	char		sr_default_text[100]		= "    default-SR ( RW): ";
	char		sr_uuid_text[100]			= "uuid ( RO)                    : ";
	char		sr_name_label_text[100]		= "              name-label ( RW): ";	
	char		sr_hostname_text[100]		= "                    host ( RO): ";
	char		sr_usage_text[100]			= "    physical-utilisation ( RO): ";	
	char		sr_size_text[100]			= "           physical-size ( RO): ";	
	char		sr_type_text[100]			= "                    type ( RO): ";
	char		sr_uuid[100];
	char		sr_default_uuid[100];
	char		sr_name_label[100];
	char		sr_hostname[100];
	char		sr_usage[30];
	char		sr_size[30];
	char		sr_type[30];
	char		sr_default_yn;
	char		vbd_uuid_text[100]			= "uuid ( RO)          : ";
	char		vdi_uuid_text[100]			= "      vdi-uuid ( RO): ";
	char		vbd_userdevice_text[100]	= "    userdevice ( RW): ";
	char		vdi_name_label_text[100]	= "name-label ( RW)      : ";
	char		vdi_virtual_size_text[100]	= "    virtual-size ( RO): ";
	char		vbd_uuid[100];
	char		vdi_uuid[100];
	char		vbd_userdevice[10];
	char		vdi_name_label[100];
	char		vdi_virtual_size[100];
	char		*string_position;
	char		dir_name[100];
	struct 		dirent *entry;
	DIR 		*dir;
	struct stat buff;
	char		file_type[100];
	char		file_name[200];
    struct tm*  lt_today;
    char		last_modify[20];
	char		xs_name_label_text[100]			= "name-label ( RW)          : ";
	char		xs_name_label[100];
	char		xs_product_version_text[100]	= "    software-version (MRO): ";
	char		xs_product_version[100];
	char		xs_build_no[100];
	char		xs_cpu_info_text[100]			= "            cpu_info (MRO): ";
	char		xs_cpu_count[100];
	char		xs_socket_count[100];
	char		xs_cpu_modelname[100];
	char		xs_ipaddress_text[100]			= "             address ( RO): ";
	char		xs_ipaddress[100];
	char		xs_memory_total_text[100]		= "        memory-total ( RO): ";
	char		xs_memory_total[100];
	char		xs_memory_free_text[100]		= "         memory-free ( RO): ";
	char		xs_memory_free[100];
	char		xs_edition_text[100]			= "             edition ( RO): ";
	char		xs_edition[100];	
	char		vm_info_name_label_text[100] 	= "name-label ( RW)       : ";
	char		vm_info_power_state_text[100]	= "      power-state ( RO): ";
	char		vm_info_memory_actual_text[100]	= "    memory-actual ( RO): ";
	char		vm_info_vcpu_max_text[100]		= "        VCPUs-max ( RW): ";
	char		vm_info_install_time_text[100]	= "     install-time ( RO): ";
	char		vm_info_os_version_text[100]	= "       os-version (MRO): ";
	char		vm_info_ipaddress_text[100]		= "         networks (MRO): ";
	char		vm_info_name_label[100];
	char		vm_info_power_state[100];
	char		vm_info_memory_actual[100];
	char		vm_info_vcpu_max[100];
	char		vm_info_install_time[100];
	char		vm_info_os_version[100];
	char		vm_info_ipaddress[100];
	char		res_vm_uuid_text[100] 			= "uuid ( RO)          : ";
	char		res_vm_name_label_text[100] 	= "    name-label ( RW): ";

	memset(temp, 0x00, sizeof(temp));
	if (memcmp(backup_msg->msg_text, "TASK", strlen("TASK")) == 0)
	{
		memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
		sprintf(backup_msg->msg_text, "\n");
		
		for (i = 0; i < MAX_MSG_CNT; i++)
		{
			// if ((backup_info[i]->status != 1) && (backup_info[i]->status != 2) && (backup_info[i]->status != 3))	// 0 : 미사용, 8 : Queue full error, 9: Error
			// {
			//	continue;
			// }
			sprintf(temp, "[%d]:%d-%ld-%c-[%.*s][%s]\n", i, backup_info[i]->status, backup_info[i]->queue_seq, backup_info[i]->task_type, sizeof(backup_info[i]->msg_seq), backup_info[i]->msg_seq, backup_info[i]->msg_text);
			// null로 인한 로그가 안보이는 문제 때문에 NULL -> space로 변경
			for (j = 0; j < sizeof(temp); j++)
			{
				if (temp[j] == 0x00)
					temp[j] = ' ';
				if (temp[j] == '\n')
					break;	
			}
			strcat(backup_msg->msg_text, temp);
		}
		backup_msg->task_type	=	'0';							
		return TRUE;
	}
	else if (memcmp(backup_msg->msg_text, "STATUS", strlen("STATUS")) == 0)
	{
		memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
		memset(temp,					0x00, 	sizeof(temp));
		// queue, 처리중인 task 검색
		sync[0] = 0; 	sync[1] = 0;
		async[0] = 0; 	async[1] = 0;
		mgt[0] = 0; 	mgt[1] = 0;
		for (i = 0; i < MAX_MSG_CNT; i++)
		{
			if ((backup_info[i]->status != 1) && (backup_info[i]->status != 2) && (backup_info[i]->status != 3))	// 0 : 미사용, 8 : Queue full error, 9: Error
			{
				continue;
			}
			if ((backup_info[i]->status == 1) || (backup_info[i]->status == 2))			// Queue 대기
			{
				switch (backup_info[i]->task_type)
				{
						case 'S' : sync[0]++;
							       break;
						case 'A' : async[0]++;
							       break;
						case 'Q' :
						case 'C' :
						case 'T' :
						case 'I' :
								   mgt[0]++;
							       break;
						// default	 : BS_Tsyslog("MONITOR internal error [%c]\n", backup_info[i]->task_type);				
				}
			}
    	
			if (backup_info[i]->status == 3)											// 처리중
			{
				switch (backup_info[i]->task_type)
				{
						case 'S' : sync[1]++;
							       break;
						case 'A' : async[1]++;
							       break;
						case 'Q' :
						case 'C' :
						case 'T' :
						case 'I' :
								   mgt[1]++;
							       break;
						// default	 : BS_Tsyslog("MONITOR internal error [%c]\n", backup_info[i]->task_type);				
				}
			}
		}

		/* 요청 format 
		{
			 "health_check_status":{
			  "xen_server":"A",
			  "storage_cloud":"A",
			  "dom0_cpu_usage":"0.00%/30.00%",
			  "dom0_disk_usage":"56%",
			  "dom0_mem_usage":"20%"
			  },
			  "sync_thread_status":{ 
			  "q/p_cnt":"0/0",
			  "thread_status":[
			   {"seq":"1","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"},
			   {"seq":"2","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"},
			   {"seq":"3","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"}
			  ]
			 },
			 "async_thread_status":{
			  "q/p_cnt":"0/0",
			              "thread_status":[
			   {"seq":"1","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"},
			   {"seq":"2","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"},
			   {"seq":"3","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"}
			  ]
			 },
			 "mgmt_thread_status":{
			  "q/p_cnt":"0/0",
			  "thread_status":[
			   {"seq":"1","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"},
			   {"seq":"2","status":"S","time":"20141028121000","elapsed_time":"0.000001","command":"df-k"}
			  ]
			 },
			 "agent_info":{
			  "ver_info":"20131014-11" 
			 }
		}
		*/
		// 일반정보
		sprintf(temp, "{\"health_check_status\":{\"xen_server\":\"%s\",\"storage_cloud\":\"%s\",\"dom0_cpu_usage\":\"%.2f%/%.2f%\",\"dom0_disk_usage\":\"%d%\",\"dom0_mem_usage\":\"%.2f%\"},", 
		                health_check.xenserver, health_check.storage, health_check.cpu_usage, cpu_limit, health_check.disk_usage, health_check.mem_usage);
		strcat(backup_msg->msg_text, temp);

		// Sync thread 정보
		sprintf(temp, "\"sync_thread_status\":{\"q_p_cnt\":\"%d/%d\",\"thread_status\":[", sync[0], sync[1]);
		strcat(backup_msg->msg_text, temp);
		
		// 각 라인별로 , 추가와 아무런 자료가 없는 경우를 대비한 flag
		// SYNC
		first	=	TRUE;
		for (i = 0; i < MAX_SYNC_THREAD_CNT; i++)
		{
			if ((sync_thread[i].run_flag[0] == 'A') || (sync_thread[i].run_flag[0] == 'S'))
			{
				if (first	==	FALSE)
				{
					strcat(backup_msg->msg_text, ",");
				}
				sprintf(temp, "{\"seq\":\"%d\",\"status\":\"%s\",\"time\":\"%s\",\"elapsed_time\":\"%d.%06ld\",\"command\":\"%s\"}", i+1, sync_thread[i].run_flag, sync_thread[i].last_time2, sync_thread[i].sec, sync_thread[i].msec, sync_thread[i].msg_text);
				strcat(backup_msg->msg_text, temp);
				first	=	FALSE;
			}
		}
		strcat(backup_msg->msg_text, "]},");

		// Async thread 정보
		sprintf(temp, "\"async_thread_status\":{\"q_p_cnt\":\"%d/%d\",\"thread_status\":[", async[0], async[1]);
		strcat(backup_msg->msg_text, temp);
		
		// 각 라인별로 , 추가와 아무런 자료가 없는 경우를 대비한 flag
		// ASYNC
		first	=	TRUE;
		for (i = 0; i < MAX_ASYNC_THREAD_CNT; i++)
		{
			if ((async_thread[i].run_flag[0] == 'A') || (async_thread[i].run_flag[0] == 'S') || (async_thread[i].run_flag[0] == 'P'))
			{
				if (first	==	FALSE)
				{
					strcat(backup_msg->msg_text, ",");
				}
				sprintf(temp, "{\"seq\":\"%d\",\"status\":\"%s\",\"time\":\"%s\",\"elapsed_time\":\"%d.%06ld\",\"command\":\"%s\"}", i+1, async_thread[i].run_flag, async_thread[i].last_time2, async_thread[i].sec, async_thread[i].msec, async_thread[i].msg_text);
				strcat(backup_msg->msg_text, temp);
				first	=	FALSE;
			}
		}
		strcat(backup_msg->msg_text, "]},");
		
		// Management thread 정보
		sprintf(temp, "\"mgmt_thread_status\":{\"q_p_cnt\":\"%d/%d\",\"thread_status\":[", mgt[0], mgt[1]);
		strcat(backup_msg->msg_text, temp);
		
		// first : 각 라인별로 , 추가와 아무런 자료가 없는 경우를 대비한 flag
		// Management
		first	=	TRUE;
		for (i = 0; i < MAX_SYNC_THREAD_CNT; i++)
		{
			if ((mgt_thread[i].run_flag[0] == 'A') || (mgt_thread[i].run_flag[0] == 'S'))
			{
				if (first	==	FALSE)
				{
					strcat(backup_msg->msg_text, ",");
				}
				sprintf(temp, "{\"seq\":\"%d\",\"status\":\"%s\",\"time\":\"%s\",\"elapsed_time\":\"%d.%06ld\",\"command\":\"%s\"}", i+1, mgt_thread[i].run_flag, mgt_thread[i].last_time2, mgt_thread[i].sec, mgt_thread[i].msec, mgt_thread[i].msg_text);
				strcat(backup_msg->msg_text, temp);
				first	=	FALSE;
			}
		}
		strcat(backup_msg->msg_text, "]},");

		//agent 정보
		sprintf(temp, "\"agent_info\":{\"ver_info\":\"%s\"}}", current_version);
		strcat(backup_msg->msg_text, temp);
		backup_msg->task_type	=	'0';							
		return TRUE;
	}
	else if (memcmp(backup_msg->msg_text, "INFO", strlen("INFO")) == 0)
	{
		memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
		memset(temp,					0x00, 	sizeof(temp));
		
		/* 요청 format
		{	
		"pool_host_info":[
			{"pool_uuid":"pool1","host_uuid":"host1","host_name":"VBR-VMhost1","host_ip":"30.30.1.171"},
			{"pool_uuid":"pool1","host_uuid":"host2","host_name":"VBR-VMhost2","host_ip":"30.30.1.172"},
			{"pool_uuid":"pool1","host_uuid":"host3","host_name":"VBR-VMhost3","host_ip":"30.30.1.173"}		
		],
		"pool_vm_info":[
			{"pool_uuid":"pool1","vm_uuid":"vm1","vm_name":"vmname1","vm_state":"halted","host_uuid":"host1"},
			{"pool_uuid":"pool1","vm_uuid":"vm2","vm_name":"vmname2","vm_state":"running","host_uuid":"host2"},
			{"pool_uuid":"pool1","vm_uuid":"vm3","vm_name":"vmname3","vm_state":"running","host_uuid":"host3"}
		]
		}
		*/

		// Xenserver 상태 check해서 'F'이면 바로 에러 처리, 정상이어도 만일을 대비해서 dummy를 통해 timeout 처리
		if (health_check.xenserver[0] != 'A')
		{
			strcpy(backup_msg->msg_text, "Xenserver is not available.");
			BS_Tsyslog("Xenserver is not available.  INFO command skip.\n");		
			return 	FALSE;	
		}
		
		// default timeout value는 10으로 설정 (필요시 cfg file로 정의하는 것을 검토)
		li_timeout	=	10;
		
		// 1단계 POOL 정보 검색		
		memset(command,	0x00,	sizeof(command));
		sprintf(command, "%s", "xe pool-list params=uuid,name-label");
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		memset(buffer, 0x00, sizeof(buffer));
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
				if (memcmp(buffer, pool_uuid_text, strlen(pool_uuid_text)) == 0)
				{
					strcpy(pool_uuid, &buffer[strlen(pool_uuid_text)]);	
					BS_Tsyslog("POOL UUID = [%s]\n", pool_uuid);
				}
				if (memcmp(buffer, pool_name_label_text, strlen(pool_name_label_text)) == 0)
				{
					strcpy(pool_name_label, &buffer[strlen(pool_name_label_text)]);	
					BS_Tsyslog("POOL Name-label = [%s]\n", pool_name_label);
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while

		// 2단계 Xenserver 정보 검색
		// 각 라인별로 , 추가와 아무런 자료가 없는 경우를 대비한 flag
		// INFO
		first	=	TRUE;
		strcat(backup_msg->msg_text, "{\"pool_host_info\":[");
		memset(command,	0x00,	sizeof(command));
		sprintf(command, "%s", "xe host-list params=uuid,name-label,address");
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		memset(buffer, 0x00, sizeof(buffer));
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
// BS_Tsyslog("DEBUG : [%s]\n", buffer);			
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
				// xenserver host uuid line
				if (memcmp(buffer, host_uuid_text, strlen(host_uuid_text)) == 0)
				{
					strcpy(host_uuid, &buffer[strlen(host_uuid_text)]);
//					BS_Tsyslog("HOST UUID = [%s]\n", host_uuid);
				}
				// xenserver name label line
				if (memcmp(buffer, host_name_label_text, strlen(host_name_label_text)) == 0)
				{
					strcpy(host_name_label, &buffer[strlen(host_name_label_text)]);
				}
				// xenserver management network label line
				if (memcmp(buffer, host_address_text, strlen(host_address_text)) == 0)
				{
					strcpy(host_ip, &buffer[strlen(host_address_text)]);
					if (first	==	FALSE)
					{
						strcat(backup_msg->msg_text, ",");
					}
					sprintf(temp, "{\"pool_uuid\":\"%s\",\"pool_name\":\"%s\",\"host_uuid\":\"%s\",\"host_name\":\"%s\",\"host_ip\":\"%s\"}", pool_uuid, pool_name_label, host_uuid, host_name_label, host_ip);
					strcat(backup_msg->msg_text, temp);
//					BS_Tsyslog("+++Curr msg = [%s]\n", backup_msg->msg_text);
					first	=	FALSE;								
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while
		strcat(backup_msg->msg_text, "],");
//							BS_Tsyslog("++++Curr msg = [%s]\n", backup_msg->msg_text);

		// 3단계 VM 정보 검색
		// 각 라인별로 , 추가와 아무런 자료가 없는 경우를 대비한 flag
		// INFO
		first	=	TRUE;
		strcat(backup_msg->msg_text, "\"pool_vm_info\":[");

		memset(command,	0x00,	sizeof(command));
		sprintf(command, "%s", "xe vm-list params=uuid,name-label,resident-on,networks,os-version,power-state is-a-template=false is-control-domain=false");
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		memset(buffer, 0x00, sizeof(buffer));
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
//				BS_Tsyslog("DEBUG = [%s]\n", buffer);
				// vm uuid line
				if (memcmp(buffer, vm_uuid_text, strlen(vm_uuid_text)) == 0)
				{
					strcpy(vm_uuid, &buffer[strlen(vm_uuid_text)]);
//					BS_Tsyslog("VM UUID [%s]\n", vm_uuid);
				}
				// vm name label line
				if (memcmp(buffer, vm_name_label_text, strlen(vm_name_label_text)) == 0)
				{
					strcpy(vm_name_label, &buffer[strlen(vm_name_label_text)]);
//					BS_Tsyslog("VM Name label [%s]\n", vm_name_label);					
				}
				// power state line
				if (memcmp(buffer, power_state_text, strlen(power_state_text)) == 0)
				{					
					strcpy(power_state, &buffer[strlen(power_state_text)]);
//					BS_Tsyslog("VM Status [%s]\n", power_state);
					// xenserver search logic
					// running이 아니면 skip
					if (memcmp(power_state, "running", strlen("running")) != 0)
					{
						// running이 아니면 NONE으로 강제 설정, 실제로는 <not in database> 표현됨
						strcpy(resident_on, "NONE");
					}
					else
					{
						// running인 경우는 resident-on 에서 해당 xenserver uuid를 설정함
					}
				}
				// resident-on line
				if (memcmp(buffer, resident_on_text, strlen(resident_on_text)) == 0)
				{
					if (memcmp(power_state, "running", strlen("running")) != 0)
					{
						// running이 아니면 power state에서 NONE으로 강제 설정, 실제로는 <not in database> 표현됨
					}
					else
					{
						strcpy(resident_on, &buffer[strlen(resident_on_text)]);
					}
				}
				// os-version line
				if (memcmp(buffer, os_version_text, strlen(os_version_text)) == 0)
				{
					if (memcmp(&buffer[strlen(os_version_text)], "<not in database>", strlen("<not in database>")) == 0)
					{
						// OS version이 정상적으로 확인되지 않은 경우.   PV가 설치되지 않았거나 기타 이유로 인한 경우임
						strcpy(os_version, "NO DEFINE");
					}
					else
					{
						// Windows OS의 경우 pattern : name: Microsoft Windows Server 2008 R2 Enterprise |C:\Windows|\Device\Harddisk0\Partition2; distro: windows; major: 6; minor: 1; spmajor: 1; spminor: 0
						if (memcmp(&buffer[strlen(os_version_text)], "name: Microsoft", strlen("name: Microsoft")) == 0)
						{
							strcpy(temp, &buffer[strlen(os_version_text) + strlen("name: ")]);
							string_position	=	NULL;
							string_position = strchr(temp, '|');
							if (string_position		==	NULL)
							{
								strcpy(os_version, "Windows OS(NOT Confirmed)");
							}
							else
							{
								memset(os_version, 0x00, sizeof(os_version));
								strncpy(os_version, temp, strlen(temp) - strlen(string_position));
							}
//							BS_Tsyslog("WINDOWS OS type = [%s][%s][%s]\n", temp, string_position, os_version);
						}
						// Linux OS의 경우 pattern : name: CentOS release 5.9 (Final); uname: 2.6.18-348.el5xen; distro: centos; major: 5; minor: 9
						else if (memcmp(&buffer[strlen(os_version_text)], "name: ", strlen("name: ")) == 0)
						{
							strcpy(temp, &buffer[strlen(os_version_text) + strlen("name: ")]);
							string_position	=	NULL;
							string_position = strchr(temp, ';');
							if (string_position		==	NULL)
							{
								strcpy(os_version, "LINUX OS(NOT Confirmed)");
							}
							else
							{
								memset(os_version, 0x00, sizeof(os_version));
								strncpy(os_version, temp, strlen(temp) - strlen(string_position));
							}
//							BS_Tsyslog("Linux OS type = [%s][%s][%s]\n", temp, string_position, os_version);							
						}
						else
						{
							// 기타 미확인 OS string
							strcpy(os_version, "NOT CHECKED");
//							BS_Tsyslog("NOT Defined OS type\n");
						}
					}
				}
				
				// network ip address line
				if (memcmp(buffer, networks_text, strlen(networks_text)) == 0)
				{
					if (memcmp(&buffer[strlen(networks_text)], "<not in database>", strlen("<not in database>")) == 0)
					{
						// ip address가 정의되지 않거나 정상적으로 확인되지 않은 경우.   PV가 설치되지 않았거나 기타 이유로 인한 경우임
						strcpy(networks, "NO DEFINE");
					}
					else
					{
						// networks_text : 0/ip: 192.168.1.223 또는 0/ip: 192.168.1.20; 0/ipv6/0: fe80:0000:0000:0000:c09e:24fb:c4a4:2247 등등 다양하게
						// 맨처음 ip만 저장
						sscanf(&buffer[strlen(networks_text)], "%s %s", temp, networks);
						if (networks[strlen(networks)-1] == ';')
						{
							networks[strlen(networks)-1] = 0x00;
						}
//						BS_Tsyslog("IP ADDRESS = [%s][%s]\n", &buffer[strlen(networks_text)], networks);													
					}		
					if (first	==	FALSE)
					{
						strcat(backup_msg->msg_text, ",");
					}
					sprintf(temp, "{\"pool_uuid\":\"%s\",\"vm_uuid\":\"%s\",\"vm_name\":\"%s\",\"vm_state\":\"%s\",\"host_uuid\":\"%s\",\"os_version\":\"%s\",\"ip_address\":\"%s\"}", pool_uuid, vm_uuid, vm_name_label, power_state, resident_on, os_version, networks);
					strcat(backup_msg->msg_text, temp);
					first	=	FALSE;
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while
		strcat(backup_msg->msg_text, "]}");
		backup_msg->task_type	=	'0';
		BS_Tsyslog("INFO TASK : [%ld][%s]\n", strlen(backup_msg->msg_text), backup_msg->msg_text);											
		return TRUE;
	}
	else if (memcmp(backup_msg->msg_text, "SR-INFO", strlen("SR-INFO")) == 0)
	{
		memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
		memset(temp,					0x00, 	sizeof(temp));
		
		/* 요청 format
		{ 
		
		  "sr_info":[
		  {"pool_uuid":"pool1","sr_uuid":"xxxxx1","sr_name":"xxxxx1","sr_type":"xxxxx1","sr_size":"xxxxx1","default":"Y","host_uuid":"xxxxx1"},
		  {"pool_uuid":"pool1","sr_uuid":"xxxxx2","sr_name":"xxxxx2","sr_type":"xxxxx2","sr_size":"xxxxx2","default":"N","host_uuid":"xxxxxx2"},
		  {"pool_uuid":"pool1","sr_uuid":"xxxxx3","sr_name":"xxxxx3","sr_type":"xxxxx3","sr_size":"xxxxx3","default":"N","host_uuid":"xxxxx3"}  
		 ]
		}
		*/

		// Xenserver 상태 check해서 'F'이면 바로 에러 처리, 정상이어도 만일을 대비해서 dummy를 통해 timeout 처리
		if (health_check.xenserver[0] != 'A')
		{
			strcpy(backup_msg->msg_text, "Xenserver is not available.");
			BS_Tsyslog("Xenserver is not available.  SR-INFO command skip.\n");		
			return 	FALSE;	
		}

		// 1단계 POOL 정보 검색		
		memset(command,	0x00,	sizeof(command));
		sprintf(command, "%s", "xe pool-list params=uuid,default-SR");
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		while ((result = msg_cut(command, buffer)) == TRUE)
		{		
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
				if (memcmp(buffer, sr_pool_uuid_text, strlen(sr_pool_uuid_text)) == 0)
				{
					strcpy(pool_uuid, &buffer[strlen(sr_pool_uuid_text)]);	
//					BS_Tsyslog("POOL UUID = [%s]\n", pool_uuid);
				}
				if (memcmp(buffer, sr_default_text, strlen(sr_default_text)) == 0)
				{
					strcpy(sr_default_uuid, &buffer[strlen(sr_default_text)]);	
//					BS_Tsyslog("POOL Default UUID = [%s]\n", sr_default_uuid);
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while

		// 2단계 sr 정보 검색
		// 각 라인별로 , 추가와 아무런 자료가 없는 경우를 대비한 flag
		// INFO
		first	=	TRUE;
		strcat(backup_msg->msg_text, "{\"sr_info\":[");
		memset(command,	0x00,	sizeof(command));
		sprintf(command, "%s", "xe sr-list params=uuid,host,name-label,physical-utilisation,physical-size,type");
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
				// SR uuid line
				if (memcmp(buffer, sr_uuid_text, strlen(sr_uuid_text)) == 0)
				{
					strcpy(sr_uuid, &buffer[strlen(sr_uuid_text)]);
//					BS_Tsyslog("SR UUID = [%s]\n", sr_uuid);
					// default SR인 경우
					if (memcmp(sr_uuid, sr_default_uuid, strlen(sr_uuid)) == 0)
					{
						sr_default_yn	=	'Y';
					}
					else
					{
						sr_default_yn	=	'N';						
					}
//					BS_Tsyslog("DEFAULT SR(Y/N) = [%c]\n", sr_default_yn);
				}
				// SR name label line
				if (memcmp(buffer, sr_name_label_text, strlen(sr_name_label_text)) == 0)
				{
					strcpy(sr_name_label, &buffer[strlen(sr_name_label_text)]);
//					BS_Tsyslog("SR NAME LABEL = [%s]\n", sr_name_label);
				}
				// SR hostname line
				if (memcmp(buffer, sr_hostname_text, strlen(sr_hostname_text)) == 0)
				{
					strcpy(sr_hostname, &buffer[strlen(sr_name_label_text)]);
//					BS_Tsyslog("SR HOST NAME = [%s]\n", sr_hostname);
				}
				// SR usage(physical-utilisation) line
				if (memcmp(buffer, sr_usage_text, strlen(sr_usage_text)) == 0)
				{
					strcpy(sr_usage, &buffer[strlen(sr_usage_text)]);
//					BS_Tsyslog("SR usage = [%s]\n", sr_usage);
				}
				// SR size(physical-size) line
				if (memcmp(buffer, sr_size_text, strlen(sr_size_text)) == 0)
				{
					strcpy(sr_size, &buffer[strlen(sr_size_text)]);
//					BS_Tsyslog("SR size = [%s]\n", sr_size);
				}
				// SR type line
				if (memcmp(buffer, sr_type_text, strlen(sr_type_text)) == 0)
				{
					strcpy(sr_type, &buffer[strlen(sr_type_text)]);
//					BS_Tsyslog("SR type = [%s]\n", sr_type);
								
					if ((memcmp(sr_type, "lvm", 	strlen("lvm")) == 0) ||
						(memcmp(sr_type, "nfs", 	strlen("nfs")) == 0) ||
						(memcmp(sr_type, "iscsi", 	strlen("iscsi")) == 0) ||
						(memcmp(sr_type, "hba", 	strlen("hba")) == 0))
					{
						if (first	==	FALSE)
						{
							strcat(backup_msg->msg_text, ",");
						}
						sprintf(temp, "{\"pool_uuid\":\"%s\",\"sr_uuid\":\"%s\",\"sr_name\":\"%s\",\"sr_type\":\"%s\",\"sr_size\":\"%s\",\"sr_usage\":\"%s\",\"default\":\"%c\", \"host_name\":\"%s\"}", 
						                 pool_uuid, sr_uuid, sr_name_label, sr_type, sr_size, sr_usage, sr_default_yn, sr_hostname);
						strcat(backup_msg->msg_text, temp);
//						BS_Tsyslog("+++Curr msg = [%s]\n", backup_msg->msg_text);
						first	=	FALSE;								
					}
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while
		strcat(backup_msg->msg_text, "]}");
		backup_msg->task_type	=	'0';											
		return TRUE;
	}	
	else if (memcmp(backup_msg->msg_text, "LUN-INFO", strlen("LUN-INFO")) == 0)
	{
		// vm uuid를 key로 저장
		memset(vm_uuid, 0x00, sizeof(vm_uuid));
		sscanf(backup_msg->msg_text, "%s %s", buffer, vm_uuid);
		
		if (strlen(vm_uuid) != 36)
		{
			sprintf(backup_msg->msg_text, "VM UUID [%s][%d] is not valid.", vm_uuid, strlen(vm_uuid));
			BS_Tsyslog("VM UUID [%s][%d] is not valid.", vm_uuid, strlen(vm_uuid));		
			return 	FALSE;				
		}
		
		memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
		memset(temp,					0x00, 	sizeof(temp));
		
		/* 요청 format
		{		
		  "lun_info":[
		                {"vbd_uuid":"xxxx1","vdi_position":"0","vdi_uuid":"xxxxx","vdi_name":"xxxxx","vdi_size":"xxxxx"},
		                {"vbd_uuid":"xxxx2","vdi_position":"1","vdi_uuid":"xxxxx","vdi_name":"xxxxx","vdi_size":"xxxxx"},
		                {"vbd_uuid":"xxxx3","vdi_position":"2","vdi_uuid":"xxxxx","vdi_name":"xxxxx","vdi_size":"xxxxx"},
		                {"vbd_uuid":"xxxx4","vdi_position":"0","vdi_uuid":"xxxxx","vdi_name":"xxxxx","vdi_size":"xxxxx"},
		                {"vbd_uuid":"xxxx5","vdi_position":"1","vdi_uuid":"xxxxx","vdi_name":"xxxxx","vdi_size":"xxxxx"},
		                {"vbd_uuid":"xxxx6","vdi_position":"2","vdi_uuid":"xxxxx","vdi_name":"xxxxx","vdi_size":"xxxxx"}                         
				    ]		
		}
		*/

		// Xenserver 상태 check해서 'F'이면 바로 에러 처리, 정상이어도 만일을 대비해서 dummy를 통해 timeout 처리
		if (health_check.xenserver[0] != 'A')
		{
			strcpy(backup_msg->msg_text, "Xenserver is not available.");
			BS_Tsyslog("Xenserver is not available.  LUN-INFO command skip.\n");		
			return 	FALSE;	
		}
		
		// VBD 정보 검색
		first	=	TRUE;			// 계속 TRUE이면 vm uuid가 문제가 있는 것으로 판단함
		sprintf(backup_msg->msg_text, "{\"lun_info\":[");
		
		memset(command,	0x00,	sizeof(command));
		sprintf(command, "xe vbd-list vm-uuid=%s type=Disk params=uuid,userdevice,vdi-uuid", vm_uuid);
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
				if (memcmp(buffer, vbd_uuid_text, strlen(vbd_uuid_text)) == 0)
				{
					strcpy(vbd_uuid, &buffer[strlen(vbd_uuid_text)]);	
					// BS_Tsyslog("VBD UUID = [%s]\n", vbd_uuid);
				}
				if (memcmp(buffer, vdi_uuid_text, strlen(vdi_uuid_text)) == 0)
				{
					strcpy(vdi_uuid, &buffer[strlen(vdi_uuid_text)]);	
					// BS_Tsyslog("VBD[%s]'s VDI UUID = [%s]\n", vbd_uuid, vdi_uuid);
				}
				if (memcmp(buffer, vbd_userdevice_text, strlen(vbd_userdevice_text)) == 0)
				{
					strcpy(vbd_userdevice, &buffer[strlen(vbd_userdevice_text)]);	
					// BS_Tsyslog("VBD[%s]'s userdevice = [%s]\n", vbd_uuid, vbd_userdevice);

					// VDI 정보 조회
					memset(command2,	0x00,	sizeof(command2));
					sprintf(command2, "%s%s %s", "xe vdi-list uuid=", vdi_uuid, "params=name-label,virtual-size");
					dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command2);

					result = 0;
					while ((result = msg_cut(command2, buffer2)) == TRUE)
					{
						if (memcmp(buffer2, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
						{
							strcpy(backup_msg->msg_text, "**TIMEOUT**");
							return FALSE;
						}
						else
						{
							if (memcmp(buffer2, vdi_name_label_text, strlen(vdi_name_label_text)) == 0)
							{
								strcpy(vdi_name_label, &buffer2[strlen(vdi_name_label_text)]);	
								// BS_Tsyslog("VBD[%s]'s VDI name-label = [%s]\n", vbd_uuid, vdi_name_label);
							}
							if (memcmp(buffer2, vdi_virtual_size_text, strlen(vdi_virtual_size_text)) == 0)
							{
								strcpy(vdi_virtual_size, &buffer2[strlen(vdi_virtual_size_text)]);	
								// BS_Tsyslog("VBD[%s]'s VDI virtual size = [%s]\n", vbd_uuid, vdi_virtual_size);
							}
        				}
        				memset(buffer2,	0x00,	sizeof(buffer2));			
        			}
        			
 					if (first	==	FALSE)
					{
						strcat(backup_msg->msg_text, ",");
					}
					sprintf(temp, "{\"vbd_uuid\":\"%s\",\"vdi_position\":\"%s\",\"vdi_uuid\":\"%s\",\"vdi_name\":\"%s\",\"vdi_size\":\"%s\"}", 
					                 vbd_uuid, vbd_userdevice, vdi_uuid, vdi_name_label, vdi_virtual_size);
					strcat(backup_msg->msg_text, temp);
					first	=	FALSE;       			
				}				
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while

		strcat(backup_msg->msg_text, "]}");
		backup_msg->task_type	=	'0';											
		return TRUE;
	}
	else if (memcmp(backup_msg->msg_text, "DIR-INFO", strlen("DIR-INFO")) == 0)
	{
		sscanf(backup_msg->msg_text, "%s %s", buffer, dir_name);

		dir = opendir (dir_name);
		if (dir == NULL)
		{
			sprintf(backup_msg->msg_text, "Directory [%s][%d] is not valid.", dir_name, strlen(dir_name));
			BS_Tsyslog("Directory [%s][%d] is not valid.", dir_name, strlen(dir_name));		
			return 	FALSE;				
		}
		
		memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
		memset(temp,					0x00, 	sizeof(temp));
		
		/* 요청 format
		{		
		  "dir_info":[
		                {"name":"file1","type":"File","size":"xxxxx","last_update":"2014-11-26 12:42:00"},
		                {"name":"dir1","type":"Directory","size":"xxxxx","last_update":"2014-11-26 12:42:00"},
				    ]		
		}
		*/

		// directory 정보 검색
		first	=	TRUE;
		strcpy(backup_msg->msg_text, "{\"dir_info\":[");
//		chdir(dir_name);		<= chdir은 thread safety 하지 않으므로 쓰지 말아야 함
		while ((entry = readdir (dir)) != NULL)
		{
			sprintf(file_name, "%s/%s", dir_name, entry->d_name);
			if (stat(file_name, &buff) < 0)
			{
				BS_Tsyslog("lstat internal error[%s][%d][%s]!!!\n", file_name, errno, strerror(errno));
				continue;
			}
			if (S_ISREG(buff.st_mode))
			{
				strcpy(file_type, 	"File");
			}
			else if (S_ISDIR(buff.st_mode))
			{
				strcpy(file_type, 	"Directory");
			}
			else if (S_ISCHR(buff.st_mode))
			{
				strcpy(file_type, 	"Character special");
			}
			else if (S_ISBLK(buff.st_mode))
			{
				strcpy(file_type, 	"Block special");
			}
			else if (S_ISFIFO(buff.st_mode))
			{
				strcpy(file_type, 	"Fifo");
			}
			else if (S_ISLNK(buff.st_mode))
			{
				strcpy(file_type, 	"Symbolic link");
			}
			else if (S_ISSOCK(buff.st_mode))
			{
				strcpy(file_type, 	"Socket");
			}
			else
			{
				strcpy(file_type, 	"Unknown");
			}
			lt_today = localtime(&buff.st_mtime);
							
    		sprintf(last_modify,"%04d-%02d-%02d %02d:%02d:%02d",
        	                    lt_today->tm_year + 1900,
        	                    lt_today->tm_mon + 1,
        	                    lt_today->tm_mday,
        	                    lt_today->tm_hour,
        	                    lt_today->tm_min,
        	                    lt_today->tm_sec);
 			if (first	==	FALSE)
			{
				strcat(backup_msg->msg_text, ",");
			}
			sprintf(temp, "{\"name\":\"%s\",\"type\":\"%s\",\"size\":\"%ld\",\"last_update\":\"%s\"}", 
			                 entry->d_name, file_type, buff.st_size, last_modify);
			strcat(backup_msg->msg_text, temp);
			first	=	FALSE;       			
		} // while

		strcat(backup_msg->msg_text, "]}");
		backup_msg->task_type	=	'0';											
		return TRUE;
	}	
	else if (memcmp(backup_msg->msg_text, "XS-INFO", strlen("XS-INFO")) == 0)
	{
		sscanf(backup_msg->msg_text, "%s %s", buffer, host_uuid);
    	
		if (strlen(host_uuid) != 36)
		{
			strcpy(backup_msg->msg_text, "Xenserver UUID is not valid.");
			return FALSE;
		}
		
		// Xenserver 상태 check해서 'F'이면 바로 에러 처리, 정상이어도 만일을 대비해서 dummy를 통해 timeout 처리
		if (health_check.xenserver[0] != 'A')
		{
			strcpy(backup_msg->msg_text, "Xenserver is not available.");
			BS_Tsyslog("Xenserver is not available.  XS-INFO command skip.\n");		
			return 	FALSE;	
		}
		
		first	=	TRUE;
		strcpy(backup_msg->msg_text, "{\"xs_info\":[");
		memset(command,	0x00,	sizeof(command));
		sprintf(command, "%s%s", "xe host-list params=name-label,software-version,cpu_info,address,memory-total,memory-free,edition uuid=", host_uuid);
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
// BS_Tsyslog("DEBUG : [%s]\n", buffer);			
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
				// xenserver name label line
				if (memcmp(buffer, xs_name_label_text, strlen(xs_name_label_text)) == 0)
				{
					strcpy(xs_name_label, &buffer[strlen(xs_name_label_text)]);
				}
				// xenserver product version line
				if (memcmp(buffer, xs_product_version_text, strlen(xs_product_version_text)) == 0)
				{
					find_str(buffer, "product_version: ",	xs_product_version);
					find_str(buffer, "build_number: ", 		xs_build_no);
				}
				// xenserver cpu info line
				if (memcmp(buffer, xs_cpu_info_text, strlen(xs_cpu_info_text)) == 0)
				{
					find_str(buffer, "cpu_count: ",			xs_cpu_count);
					find_str(buffer, "socket_count: ", 		xs_socket_count);
					find_str(buffer, "modelname: ", 		xs_cpu_modelname);
				}
				// xenserver ip address line
				if (memcmp(buffer, xs_ipaddress_text, strlen(xs_ipaddress_text)) == 0)
				{
					strcpy(xs_ipaddress, &buffer[strlen(xs_ipaddress_text)]);
				}
				// xenserver memory total line
				if (memcmp(buffer, xs_memory_total_text, strlen(xs_memory_total_text)) == 0)
				{
					strcpy(xs_memory_total, &buffer[strlen(xs_memory_total_text)]);
				}
				// xenserver memory free line
				if (memcmp(buffer, xs_memory_free_text, strlen(xs_memory_free_text)) == 0)
				{
					strcpy(xs_memory_free, &buffer[strlen(xs_memory_free_text)]);
				}
				// xenserver edition line
				if (memcmp(buffer, xs_edition_text, strlen(xs_edition_text)) == 0)
				{
					strcpy(xs_edition, &buffer[strlen(xs_edition_text)]);

					if (first	==	FALSE)
					{
						strcat(backup_msg->msg_text, ",");
					}
					sprintf(temp, "{\"name_label\":\"%s\",\"version\":\"%s\",\"build_number\":\"%s\",\"cpu_count\":\"%s\",\"socket_count\":\"%s\",\"modelname\":\"%s\",\"ip_address\":\"%s\",\"total_memory\":\"%s\",\"free_memory\":\"%s\",\"edition\":\"%s\"}",
					                  xs_name_label, xs_product_version, xs_build_no, xs_cpu_count, xs_socket_count, xs_cpu_modelname, xs_ipaddress, xs_memory_total, xs_memory_free, xs_edition);
					strcat(backup_msg->msg_text, temp);
					first	=	FALSE;								
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while
		if (first	==	TRUE)
		{
			strcpy(backup_msg->msg_text, "Xenserver not found.");
			return FALSE;
		}
		else
		{
			strcat(backup_msg->msg_text, "]}");
			backup_msg->task_type	=	'0';	
		}										
		return TRUE;
	}
	else if (memcmp(backup_msg->msg_text, "VM-INFO", strlen("VM-INFO")) == 0)
	{
		sscanf(backup_msg->msg_text, "%s %s", buffer, vm_uuid);
    	
		if (strlen(vm_uuid) != 36)
		{
			strcpy(backup_msg->msg_text, "VM UUID is not valid.");
			return FALSE;
		}
		
		// Xenserver 상태 check해서 'F'이면 바로 에러 처리, 정상이어도 만일을 대비해서 dummy를 통해 timeout 처리
		if (health_check.xenserver[0] != 'A')
		{
			strcpy(backup_msg->msg_text, "Xenserver is not available.");
			BS_Tsyslog("Xenserver is not available.  VM-INFO command skip.\n");		
			return 	FALSE;	
		}
		
		first	=	TRUE;
		strcpy(backup_msg->msg_text, "{\"vm_info\":[");		
		memset(command,	0x00,	sizeof(command));
		sprintf(command, "%s%s", "xe vm-list params=name-label,power-state,memory-actual,VCPUs-max,install-time,networks,os-version uuid=", vm_uuid);
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
// BS_Tsyslog("DEBUG : [%s]\n", buffer);			
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
				// BS_Tsyslog("===DEBUG = [%s]\n", buffer);
				// vm name label line
				if (memcmp(buffer, vm_info_name_label_text, strlen(vm_info_name_label_text)) == 0)
				{
					strcpy(vm_info_name_label, &buffer[strlen(vm_info_name_label_text)]);
				}
				// vm power state line
				if (memcmp(buffer, vm_info_power_state_text, strlen(vm_info_power_state_text)) == 0)
				{
					// BS_Tsyslog("Power state = [%s]\n", 	vm_info_power_state);				
					strcpy(vm_info_power_state, &buffer[strlen(vm_info_power_state_text)]);
				}
				// vm memory actual line
				if (memcmp(buffer, vm_info_memory_actual_text, strlen(vm_info_memory_actual_text)) == 0)
				{
					strcpy(vm_info_memory_actual, &buffer[strlen(vm_info_memory_actual_text)]);
				}
				// vm vcpu max line
				if (memcmp(buffer, vm_info_vcpu_max_text, strlen(vm_info_vcpu_max_text)) == 0)
				{
					strcpy(vm_info_vcpu_max, &buffer[strlen(vm_info_vcpu_max_text)]);
				}
				// vm install time line
				if (memcmp(buffer, vm_info_install_time_text, strlen(vm_info_install_time_text)) == 0)
				{
					memset(vm_info_install_time,	0x00,	sizeof(vm_info_install_time));
					memcpy(vm_info_install_time, &buffer[strlen(vm_info_install_time_text)], 8);	//20141208T01:27:08Z 
					// vm_info_install_time[strlen(vm_info_install_time)-1] = 0x00;
				}
				// vm os version line
				if (memcmp(buffer, vm_info_os_version_text, strlen(vm_info_os_version_text)) == 0)
				{
					if (memcmp(&buffer[strlen(vm_info_os_version_text)], "<not in database>", strlen("<not in database>")) == 0)
					{
						// OS version이 정상적으로 확인되지 않은 경우.   PV가 설치되지 않았거나 기타 이유로 인한 경우임
						strcpy(vm_info_os_version, "NO DEFINE");
					}
					else
					{
						// Windows OS의 경우 pattern : name: Microsoft Windows Server 2008 R2 Enterprise |C:\Windows|\Device\Harddisk0\Partition2; distro: windows; major: 6; minor: 1; spmajor: 1; spminor: 0
						if (memcmp(&buffer[strlen(vm_info_os_version_text)], "name: Microsoft", strlen("name: Microsoft")) == 0)
						{
							strcpy(temp, &buffer[strlen(vm_info_os_version_text) + strlen("name: ")]);
							string_position	=	NULL;
							string_position = strchr(temp, '|');
							if (vm_info_os_version		==	NULL)
							{
								strcpy(vm_info_os_version, "Windows OS(NOT Confirmed)");
							}
							else
							{
								memset(vm_info_os_version, 0x00, sizeof(os_version));
								strncpy(vm_info_os_version, temp, strlen(temp) - strlen(string_position));
							}
							// BS_Tsyslog("WINDOWS OS type = [%s][%s][%s]\n", temp, string_position, os_version);
						}
						// Linux OS의 경우 pattern : name: CentOS release 5.9 (Final); uname: 2.6.18-348.el5xen; distro: centos; major: 5; minor: 9
						else if (memcmp(&buffer[strlen(vm_info_os_version_text)], "name: ", strlen("name: ")) == 0)
						{
							strcpy(temp, &buffer[strlen(vm_info_os_version_text) + strlen("name: ")]);
							string_position	=	NULL;
							string_position = strchr(temp, ';');
							if (string_position		==	NULL)
							{
								strcpy(vm_info_os_version, "LINUX OS(NOT Confirmed)");
							}
							else
							{
								memset(vm_info_os_version, 0x00, sizeof(os_version));
								strncpy(vm_info_os_version, temp, strlen(temp) - strlen(string_position));
							}
							// BS_Tsyslog("Linux OS type = [%s][%s][%s]\n", temp, string_position, os_version);							
						}
						else
						{
							// 기타 미확인 OS string
							strcpy(vm_info_os_version, "NOT CHECKED");
							// BS_Tsyslog("NOT Defined OS type\n");
						}
					}
				}
				// vm ipaddress line
				if (memcmp(buffer, vm_info_ipaddress_text, strlen(vm_info_ipaddress_text)) == 0)
				{
					if (memcmp(&buffer[strlen(vm_info_ipaddress_text)], "<not in database>", strlen("<not in database>")) == 0)
					{
						// ip address가 정의되지 않거나 정상적으로 확인되지 않은 경우.   PV가 설치되지 않았거나 기타 이유로 인한 경우임
						strcpy(vm_info_ipaddress, "NO DEFINE");
					}
					else
					{
						// networks_text : 0/ip: 192.168.1.223 또는 0/ip: 192.168.1.20; 0/ipv6/0: fe80:0000:0000:0000:c09e:24fb:c4a4:2247 등등 다양하게
						// 맨처음 ip만 저장
						sscanf(&buffer[strlen(vm_info_ipaddress_text)], "%s %s", temp, vm_info_ipaddress);
						if (vm_info_ipaddress[strlen(vm_info_ipaddress)-1] == ';')
						{
							vm_info_ipaddress[strlen(vm_info_ipaddress)-1] = 0x00;
						}
//						BS_Tsyslog("IP ADDRESS = [%s][%s]\n", &buffer[strlen(networks_text)], networks);													
					}		

					if (first	==	FALSE)
					{
						strcat(backup_msg->msg_text, ",");
					}
					sprintf(temp, "{\"name_label\":\"%s\",\"os_version\":\"%s\",\"power_state\":\"%s\",\"vcpu_cnt\":\"%s\",\"memory\":\"%s\",\"install_time\":\"%s\",\"ip_address\":\"%s\"}",
					                  vm_info_name_label, vm_info_os_version, vm_info_power_state, vm_info_vcpu_max, vm_info_memory_actual, vm_info_install_time, vm_info_ipaddress);
					strcat(backup_msg->msg_text, temp);
					first	=	FALSE;								
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while
		if (first	==	TRUE)
		{
			strcpy(backup_msg->msg_text, "VM not found.");
			return FALSE;
		}
		else
		{
			strcat(backup_msg->msg_text, "]}");
			backup_msg->task_type	=	'0';	
		}										
		return TRUE;
	}
	else if (memcmp(backup_msg->msg_text, "RES-VM-INFO", strlen("RES-VM-INFO")) == 0)
	{
		sscanf(backup_msg->msg_text, "%s %s", buffer, host_uuid);

		// xenserver uuid가 지정되거나 NONE으로 현재 중단되어 있는 VM만 조회할 떄만 정상 처리    	
		if ((strlen(host_uuid) != 36) && (memcmp(host_uuid, "NONE", strlen("NONE")) != 0))
		{
			sprintf(backup_msg->msg_text, "Xenserver UUID is not valid.[%s]", host_uuid);
			return FALSE;
		}

		memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
		memset(temp,					0x00, 	sizeof(temp));
		
		/* 요청 format
		{	
		"res_vm_info":[
			{"pool_uuid":"pool1","vm_uuid":"vm1","vm_name":"vmname1","vm_state":"halted","host_uuid":"host1"},
			{"pool_uuid":"pool1","vm_uuid":"vm2","vm_name":"vmname2","vm_state":"running","host_uuid":"host2"},
			{"pool_uuid":"pool1","vm_uuid":"vm3","vm_name":"vmname3","vm_state":"running","host_uuid":"host3"}
		]
		}
		*/

		// Xenserver 상태 check해서 'F'이면 바로 에러 처리, 정상이어도 만일을 대비해서 dummy를 통해 timeout 처리
		if (health_check.xenserver[0] != 'A')
		{
			strcpy(backup_msg->msg_text, "Xenserver is not available.");
			BS_Tsyslog("Xenserver is not available.  INFO command skip.\n");		
			return 	FALSE;	
		}
		
		// default timeout value는 10으로 설정 (필요시 cfg file로 정의하는 것을 검토)
		li_timeout	=	10;
		
		first	=	TRUE;
		strcat(backup_msg->msg_text, "{\"res_vm_info\":[");
		memset(command,	0x00,	sizeof(command));
		// Xenserver UUID가 지정된 경우
		if (strlen(host_uuid) == 36)
		{
			sprintf(command, "%s%s", "xe vm-list params=uuid,name-label is-a-template=false is-control-domain=false resident-on=", host_uuid);
		}
		else
		{
			sprintf(command, "%s",   "xe vm-list params=uuid,name-label is-a-template=false is-control-domain=false power-state=halted");			
		}
		dummy_call(mgt_value + thd_seq, HEALTH_CHECK_INTERVAL, command);

		result = 0;
		while ((result = msg_cut(command, buffer)) == TRUE)
		{
			if (memcmp(buffer, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				strcpy(backup_msg->msg_text, "**TIMEOUT**");
				return FALSE;
			}
			else
			{
//				BS_Tsyslog("DEBUG = [%s]\n", buffer);
				// vm uuid line
				if (memcmp(buffer, res_vm_uuid_text, strlen(res_vm_uuid_text)) == 0)
				{
					strcpy(vm_uuid, &buffer[strlen(res_vm_uuid_text)]);
//					BS_Tsyslog("VM UUID [%s]\n", vm_uuid);
				}
				// vm name label line
				if (memcmp(buffer, res_vm_name_label_text, strlen(res_vm_name_label_text)) == 0)
				{
					strcpy(vm_name_label, &buffer[strlen(res_vm_name_label_text)]);
//					BS_Tsyslog("VM Name label [%s]\n", vm_name_label);					
					if (first	==	FALSE)
					{
						strcat(backup_msg->msg_text, ",");
					}
					sprintf(temp, "{\"vm_uuid\":\"%s\",\"vm_name\":\"%s\"}", vm_uuid, vm_name_label);
					strcat(backup_msg->msg_text, temp);
//							BS_Tsyslog("+++++Curr msg = [%s]\n", backup_msg->msg_text);
					first	=	FALSE;
				}
			}
			memset(buffer,	0x00,	sizeof(buffer));			
		} // while
		if (first	==	TRUE)
		{
			strcpy(backup_msg->msg_text, "Xenserver (RES) not found.");
			return FALSE;
		}
		else
		{
			strcat(backup_msg->msg_text, "]}");
			backup_msg->task_type	=	'0';	
		}										
		return TRUE;
	}
	else if (memcmp(backup_msg->msg_text, "EVENT-INFO", strlen("EVENT-INFO")) == 0)
	{
		/* 요청 format
		{	
		"res_vm_info":[
			{"pool_uuid":"pool1","vm_uuid":"vm1","vm_name":"vmname1","vm_state":"halted","host_uuid":"host1"},
			{"pool_uuid":"pool1","vm_uuid":"vm2","vm_name":"vmname2","vm_state":"running","host_uuid":"host2"},
			{"pool_uuid":"pool1","vm_uuid":"vm3","vm_name":"vmname3","vm_state":"running","host_uuid":"host3"}
		]
		}
		*/

		sprintf(temp, "{\"event_info\":[");
		strcpy(backup_msg->msg_text, temp);
		
		first	=	TRUE;
		for (i = 0; i < MAX_EVENT_CNT; i++)
		{
			if (event_msg[i].status == '1')
			{
				if (first	==	FALSE)
				{
					strcat(backup_msg->msg_text, ",");
				}
				sprintf(temp, "{\"event_type\":\"%c\",\"host_name\":\"%s\",\"host_uuid\":\"%s\",\"event_time\":\"%s\",\"event_code\":\"%s\",\"event_msg\":\"%s\"}",
				                event_msg[i].event_type, g_host_name, g_host_uuid, event_msg[i].event_time, event_msg[i].event_code, event_msg[i].event_msg);
				strcat(backup_msg->msg_text, temp);
				first	=	FALSE;
				event_msg[i].status = '9';
			}
		
		} // while
		strcat(backup_msg->msg_text, "]}");
		backup_msg->task_type	=	'0';	
		return TRUE;
	}
	BS_Tsyslog("Inquiry : Invalid option[%s].\n", backup_msg->msg_text);
	sprintf(temp, "Inquiry : Invalid option[%s].\n", backup_msg->msg_text);
	strcpy(backup_msg->msg_text, temp);
	return FALSE;
}

int cancel_async_task()
{
	int	i;
	char	search_key[MAX_BUFFER_SIZE];		// 현재 작업 처리중인 task cancel용 key field
	char	temp[MAX_BUFFER_SIZE];		// 현재 작업 처리중인 task cancel용 key field
		
	for (i = 0; i < MAX_ASYNC_THREAD_CNT; i++)
	{
		if (async_thread[i].run_flag[0] == 'A')
		{
			BS_Tsyslog("Storage Cloud Infra is not available.   EXPORT/IMPORT Task [%.17s][%s] canceled.\n", async_thread[i].msg_seq, async_thread[i].msg_text);
			if ((strcmp(async_thread[i].msg_text, "RESTORE") == 0) || (strcmp(async_thread[i].msg_text, "BACKUP") == 0))
			{
				BS_Tsyslog("Storage Cloud Infra is not available.   EXPORT/IMPORT Task [%.17s] canceled.\n", async_thread[i].msg_seq);
				// sscanf(async_thread[i].msg_text, "%s %s", temp, search_key);
				/* // 일반 remote 명령인 경우 실행 유지하도록 변경
				else
				{
					sprintf(search_key, "%s", async_thread[i]->msg_text);
				}
				*/
				// kill 명령 조립
				// head -2 는 동일한 uuid를 가진 명령이 있을때 해당 명령어와 shell만 kill 하기 위해
				// 동일 uuid로 백업이 두개 존재하게 되면 먼저 실행된 명령부터 kill됨
				// 현재 문제점 : msg text에는 vm uuid가 있지만 실제 백업은 snapshot uuid, 복구는 파일이름 => key field가 없음
				// vm-export vm-import를 일괄 kill 처리함
				// if (strcmp(async_thread[i].msg_text, "RESTORE") == 0)
				// {
				// 	// sprintf(temp, "ps -ef|grep \"%s\"|grep -v grep|grep vm-import|head -2|awk '{print $2}'|xargs kill -9", search_key);
				// 	sprintf(temp, "ps -ef|grep vm-import|grep -v grep|awk '{print $2}'|xargs kill -9", search_key);
				// }
				// else if (strcmp(async_thread[i].msg_text, "BACKUP") == 0)
				// {
				// 	// sprintf(temp, "ps -ef|grep \"%s\"|grep -v grep|grep vm-export|head -2|awk '{print $2}'|xargs kill -9", search_key);
				// 	sprintf(temp, "ps -ef|grep vm-export|grep -v grep|awk '{print $2}'|xargs kill -9", search_key);
				// }
				// BS_Tsyslog("=== Cancel TASK [%.*s][%s][%s]\n", sizeof(async_thread[i].msg_seq), async_thread[i].msg_seq, search_key, temp);
				// system(temp);
				sprintf(temp, "ps -ef|grep vm-export|grep -v grep|awk '{print $2}'|xargs kill -9", search_key);
				BS_Tsyslog("=== Cancel TASK Command [%.*s][%s]\n", sizeof(async_thread[i].msg_seq), async_thread[i].msg_seq, temp);
				system(temp);
				sprintf(temp, "ps -ef|grep vm-import|grep -v grep|awk '{print $2}'|xargs kill -9", search_key);
				BS_Tsyslog("=== Cancel TASK Command [%.*s][%s]\n", sizeof(async_thread[i].msg_seq), async_thread[i].msg_seq, temp);
				system(temp);
			}
				sprintf(temp, "ps -ef|grep vm-export|grep -v grep|awk '{print $2}'|xargs kill -9", search_key);
				BS_Tsyslog("=== Cancel TASK Command [%.*s][%s]\n", sizeof(async_thread[i].msg_seq), async_thread[i].msg_seq, temp);
				system(temp);
				sprintf(temp, "ps -ef|grep vm-import|grep -v grep|awk '{print $2}'|xargs kill -9", search_key);
				BS_Tsyslog("=== Cancel TASK Command [%.*s][%s]\n", sizeof(async_thread[i].msg_seq), async_thread[i].msg_seq, temp);
				system(temp);
		}	
	}
}

// 전체 취소의 경우 전문번호 17자리가 0임 "00000000000000000"
int cancel_task(int thd_seq, BACKUP_INFO *backup_msg)
{
	int				i;
	int				total;				// 전체 취소 대상 건수
	int				cancel_success;		// 취소 성공한 대상 건수
	int				cancel_fail;		// 취소 실패한 대상 건수
	int				result;
	char			temp[MAX_BUFFER_SIZE];
//	BS_Tsyslog("=== cancel start[%d],[%s]\n", strlen(backup_msg->msg_text), backup_msg->msg_text);
	
	if (strlen(backup_msg->msg_text) != 17)							// 전문 seq가 17자리가 아니면 오류 처리
	{
		sprintf(backup_msg->msg_text, "Cancel request sequence is not valid.");
		return FALSE;	
	}
	
	if (strcmp(backup_msg->msg_text, "00000000000000000") != 0)		//단건 취소
	{
		return (cancel_task_detail((BACKUP_INFO *)backup_msg));
	}
	
	// 일괄 취소인 경우
	if (strcmp(backup_msg->msg_text, "00000000000000000") == 0)		//일괄 취소
	{
		total	=	cancel_success	=	cancel_fail	=	0;
		for (i = 0; i < MAX_MSG_CNT; i++)
		{
			// 기 처리된 전문 ('3'처리중 이상 이거나 0으로 초기화 된 경우)
			//if (backup_info[i]->status >= 3 || backup_info[i]->status == 0)
			//{
			//	continue;		
			//}
			
			// 단건 취소와 같은 유형으로 변경 : msg_text에 취소할 msg seq 복사
			memcpy(backup_msg->msg_text, backup_info[i]->msg_seq, sizeof(backup_info[i]->msg_seq));
			total+= 1;
			result	=	cancel_task_detail(backup_msg);
			if (result == FALSE)
			{
				cancel_fail		+=	1;
			}
			else
			{
				cancel_success	+=	1;	
			}
//			BS_Tsyslog("Multi cancel status : total [%d], cancel [%d], failed [%d]\n", total, cancel_success, cancel_fail);
		}
		BS_Tsyslog("Multi cancel summary : total [%d], cancel [%d], failed [%d]\n", total, cancel_success, cancel_fail);
		sprintf(backup_msg->msg_text, "Multi cancel summary : total [%d], cancel [%d], failed [%d]\n", total, cancel_success, cancel_fail);		
		return TRUE;		
	}

	// 실행되면 안되는 영역 에러
	sprintf(backup_msg->msg_text, "UNKNOWN : Cancel request sequence is not valid[%s]", backup_msg->msg_text);
	return	FALSE;	
}

int	cancel_task_detail(BACKUP_INFO *backup_msg)
{
	int		i;
	char	temp[MAX_BUFFER_SIZE];
	int		find_msg;
	int		sync_queue_id;
	int		async_queue_id;
	int		query_queue_id;
	int		queue_id;
	ssize_t nbytes;
	Msg     msg;
	char	search_key[MAX_BUFFER_SIZE];		// 현재 작업 처리중인 task cancel용 key field
	
	find_msg	=	0;
	for (i = 0; i < MAX_MSG_CNT; i++)
	{
		// 단건일때에는 msg_text에 취소 하고자 하는 전문 seq가 있음 (일반 cancel request 전문)
		// 일괄취소 일때에는 msg_text에는 취소 대상 전문에서 실행할 명령어가 있고, 전문 seq는 표준 전문 layout에 있으므로 차이가 있음
//		BS_Tsyslog("finding Message : [%.17s][%s]\n", backup_info[i]->msg_seq, (char *)&backup_msg->msg_type);
		if (memcmp(backup_info[i]->msg_seq, backup_msg->msg_text, sizeof(backup_msg->msg_seq)) == 0)		// 해당 전문을 찾은 경우
		{
			find_msg	=	1;
//			BS_Tsyslog("FIND Message : [%.17s][%d][%c]\n", backup_info[i]->msg_seq, backup_info[i]->status, backup_info[i]->task_type);
			break;
		}
	}
	
	// shared memory에서 해당 전문을 못 찾은 경우 
	if (find_msg	==	0)
	{
		strcpy(backup_msg->msg_text, "Request message not found.(Invalid or Already finished).");
		return FALSE;
	}

	// 기 처리된 전문 (3 이상 이거나 0으로 초기화 된 경우)
	if (backup_info[i]->status > 3 || backup_info[i]->status == 0)
	{
		strcpy(backup_msg->msg_text, "Request message Already finished.");
		return FALSE;		
	}
	
	// 처리중인 전문 강제 종료
	if (backup_info[i]->status == 3)
	{
		// 아래 강제 종료는 검토후 반영할 것.  실행중인 task는 일단 오류 처리함
		strcpy(backup_msg->msg_text, "Request message is processing.");
		return FALSE;
		
		//
		// 강제 종료 로직은 테스트 중... (동일 명령어가 전부 kill 되는 부분 수정해야 함)		
		backup_info[i]->status	=	9;
		// 백업 명령인 경우는 uuid로 검색
		// 기타 명령인 경우는 명령 전체를 검색
		if ((strcmp(backup_info[i]->msg_text, "RESTORE") == 0) || 
			(strcmp(backup_info[i]->msg_text, "PARTIAL") == 0) ||
			(strcmp(backup_info[i]->msg_text, "BACKUP") == 0))
		{
			sscanf(backup_info[i]->msg_text, "%s %s", temp, search_key);
		}
		else		// 일반 remote 명령인 경우
		{
			sprintf(search_key, "%s", backup_info[i]->msg_text);
		}
		
		// kill 명령 조립
		sprintf(temp, "ps -ef|grep \"%s\"|grep -v grep|awk '{print $2}'|xargs kill -9", search_key);
//		BS_Tsyslog("=== Cancel TASK [%.*s][%s][%s]\n", sizeof(backup_info[i]->msg_seq), backup_info[i]->msg_seq, search_key, temp);
		system(temp);
		strcpy(temp, (char *)&backup_info[i]->msg_type);
		// 기존 TASK cancel 처리를 위해
//		BS_Tsyslog("=== Cancel TASK2 [%.*s][%s][%s]\n", sizeof(backup_info[i]->msg_seq), backup_info[i]->msg_seq, search_key, temp);
		TRAN_LOG2('C', "TASK Canceled", "%s", temp);
		sprintf(backup_msg->msg_text, "Request message is cancel.[%.*s]", sizeof(backup_info[i]->msg_seq), backup_info[i]->msg_seq);
    	backup_msg->task_type	=	'0';
		return TRUE;		
	}
	
	// 중복 처리를 대비 status를 재조회후 변경
	if (backup_info[i]->status == 1 || backup_info[i]->status == 2)
	{
		backup_info[i]->status	=	9;
	}
	else
	{
		strcpy(backup_msg->msg_text, "[NOTICE]Request message is processing or already finished.");
		return FALSE;		
	}		
	
	// 처리 대기중인 Queue 정리를 위해 SYNC, ASYNC, MGT별 message queue id 선택
	sync_queue_id 			=	msgget(sync_msg_key, IPC_CREAT | 0644);
	async_queue_id 			=	msgget(async_msg_key, IPC_CREAT | 0644);
	query_queue_id 			=	msgget(mgt_msg_key, IPC_CREAT | 0644);

	switch (backup_info[i]->task_type)
	{
		case 'S' : queue_id 		=	sync_queue_id;
			       break;
		case 'A' : queue_id 		=	async_queue_id;
			       break;
		case 'Q' :
		case 'C' :
		case 'T' :
		case 'I' :						 
				   queue_id 		=	query_queue_id;
			       break;
		default  : 
				   strcpy(backup_msg->msg_text, "Request message type is not valid.");
				   return FALSE;
	}

	// BS_Tsyslog("CANCEL : [%c] task queue, [%ld]\n", backup_info[i]->task_type, backup_info[i]->queue_seq);	
	nbytes	=	msgrcv(queue_id, &msg, MAX_DATA_SIZE, backup_info[i]->queue_seq, IPC_NOWAIT);
	if (nbytes > 0)
	{
		strcpy(temp, (char *)&backup_info[i]->msg_type);
		// 기존 TASK cancel 처리를 위해
		TRAN_LOG2('C', "TASK Canceled", "%s", temp);
		sprintf(backup_msg->msg_text, "Request message is cancel.[%.*s]", sizeof(backup_msg->msg_seq), backup_msg->msg_seq);
    	backup_msg->task_type	=	'0';
		return TRUE;	
	}
	else
	{
		if (errno == ENOMSG)
		{
			sprintf(backup_msg->msg_text, "Queue Internal ERROR. Queue is empty.");
		}
		else
		{
			sprintf(backup_msg->msg_text, "Queue Internal ERROR [%d].", errno);
		}
		return FALSE;
	}
}

int		thread_task(int thd_seq, BACKUP_INFO *msg_info)
{ // SKM
	char			buffer[MAX_BUFFER_SIZE];
	int				curr_sync_thread;
	int				curr_async_thread;
	int				curr_mgt_thread;
	int				new_sync_thread;
	int				new_async_thread;
	int				new_mgt_thread;
	pthread_t		sync_thread_array[MAX_SYNC_THREAD_CNT];
	pthread_t		async_thread_array[MAX_ASYNC_THREAD_CNT];
	pthread_t		mgt_thread_array[MAX_MGT_THREAD_CNT];
	int				i, result;
	int				thr_id;
	int				queue_id;
	Msg				msg;
	
	curr_sync_thread = curr_async_thread = curr_mgt_thread = 0;
	memset(buffer, 0x00, sizeof(buffer));
	msg_info->task_type	=	'9';		// default로 '9' 에러로 초기화
	// 잘못된 값이 오는 경우 agent fail이 될 수 있는 가능성 점검할 것
	sscanf(msg_info->msg_text, "%d %d %d", &new_sync_thread, &new_async_thread, &new_mgt_thread);

	if ((new_sync_thread  > MAX_SYNC_THREAD_CNT)  || (new_sync_thread  < MIN_SYNC_THREAD_CNT)  ||
		(new_async_thread > MAX_ASYNC_THREAD_CNT) || (new_async_thread < MIN_ASYNC_THREAD_CNT) ||
		(new_mgt_thread   > MAX_MGT_THREAD_CNT)   || (new_mgt_thread   < MIN_MGT_THREAD_CNT))
	{
		sprintf(msg_info->msg_text, "Processing internal Error. SYNC Max=[%d]Min=[%d]:[%d], ASYNC Max=[%d]Min=[%d]:[%d], Management Max=[%d]Min=[%d]:[%d]", 
		                             MAX_SYNC_THREAD_CNT,  MIN_SYNC_THREAD_CNT,  new_sync_thread,
		                             MAX_ASYNC_THREAD_CNT, MIN_ASYNC_THREAD_CNT, new_async_thread,
		                             MAX_MGT_THREAD_CNT,   MIN_MGT_THREAD_CNT,   new_mgt_thread);
		BS_Tsyslog("Processing internal Error. SYNC Max=[%d]Min=[%d]:[%d], ASYNC Max=[%d]Min=[%d]:[%d], Management Max=[%d]Min=[%d]:[%d]\n", 
		                             MAX_SYNC_THREAD_CNT,  MIN_SYNC_THREAD_CNT,  new_sync_thread,
		                             MAX_ASYNC_THREAD_CNT, MIN_ASYNC_THREAD_CNT, new_async_thread,
		                             MAX_MGT_THREAD_CNT,   MIN_MGT_THREAD_CNT,   new_mgt_thread);
		return FALSE;
	}
	
	for (i = 0; i < MAX_SYNC_THREAD_CNT; i++)
	{
		if ((sync_thread[i].run_flag[0] == 'A') || (sync_thread[i].run_flag[0] == 'S'))
		{
			curr_sync_thread++;
		}
	}
	for (i = 0; i < MAX_ASYNC_THREAD_CNT; i++)
	{
		if ((async_thread[i].run_flag[0] == 'A') || (async_thread[i].run_flag[0] == 'S') || (async_thread[i].run_flag[0] == 'P'))
		{
			curr_async_thread++;
		}
	}
	for (i = 0; i < MAX_MGT_THREAD_CNT; i++)
	{
		if ((mgt_thread[i].run_flag[0] == 'A') || (mgt_thread[i].run_flag[0] == 'S'))
		{
			curr_mgt_thread++;			
		}
	}
	
	// thread 개수는 증가시킬때는 thread create. 감소는 해당 queue에 msg.msg_type = 0이고 msg.msg_text = "KILL"인 message를 송신한다.	
	// Sync thread 개수 조절
	if (new_sync_thread 		>	curr_sync_thread)		// thread 증가
	{
		if (new_sync_thread		>	MAX_SYNC_THREAD_CNT)	// MAX값 초과한 경우
		{
			sprintf(msg_info->msg_text, "SYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]", new_sync_thread, MAX_SYNC_THREAD_CNT,  MIN_SYNC_THREAD_CNT);
			BS_Tsyslog("SYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]\n", new_sync_thread, MAX_SYNC_THREAD_CNT,  MIN_SYNC_THREAD_CNT);
			return FALSE;			
		}
		
		for (i = 0; i < MAX_SYNC_THREAD_CNT; i++)
		{
			if ((sync_thread[i].run_flag[0] == 'A') || (sync_thread[i].run_flag[0] == 'S'))
			{
				continue;
			}
		
			thr_id	=	pthread_create(&sync_thread_array[i], NULL, sync_processing, 		(void *) &i);
    		if (thr_id < 0)
    		{
    		    sprintf(msg_info->msg_text, "Sync thread create error : [%d]th - [%d].", i, thr_id);
    		    BS_Tsyslog("Sync thread create error : [%d]th - [%d].\n", i, thr_id);
				return FALSE;			    			    
    		}
			strcpy(sync_thread[i].run_flag,    "Standby");		
			sprintf(sync_thread[i].last_time,  "0000-00-00 00:00:00");
			sprintf(sync_thread[i].last_time2, "00000000000000");
			sync_thread[i].sec				=	0;
			sync_thread[i].msec				=	0L;			
			memset(sync_thread[i].msg_text, 0x00, sizeof(sync_thread[i].msg_text));
    		// 식별번호 p_thread 를 가지는 쓰레드를 detach 
    		pthread_detach(sync_thread_array[i]);
			sleep(1);
			BS_Tsyslog("Sync thread add.  seq = [%d]\n", i);
			if (++curr_sync_thread == new_sync_thread)
			{
				break;	
			}
		}
	}
	if (new_sync_thread 		<	curr_sync_thread)		// thread 감소
	{
		if (new_sync_thread		<	MIN_SYNC_THREAD_CNT)	// MIN값 보다 작은 경우
		{
			sprintf(msg_info->msg_text, "SYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]", new_sync_thread, MAX_SYNC_THREAD_CNT,  MIN_SYNC_THREAD_CNT);
			BS_Tsyslog("SYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]\n", new_sync_thread, MAX_SYNC_THREAD_CNT,  MIN_SYNC_THREAD_CNT);
			return FALSE;			
		}
		
		for (i = 0; i < (curr_sync_thread - new_sync_thread); i++)
		{
			// queue msg_type 0, msg_text "KILL"
			queue_id 			=	msgget(sync_msg_key, IPC_CREAT | 0644);
			msg.msg_type		=	1L;
			strcpy(msg.msg_text, "KILL");
			result				=	msgsnd(queue_id, &msg, strlen(msg.msg_text), 0);
			if (result == -1)
			{
				BS_Tsyslog("Sync Queue request failed(KILL), send ERROR %d, error message = [%s]\n", errno, strerror(errno));
				sprintf(msg_info->msg_text, "Sync Queue request failed(KILL), send ERROR %d, error message = [%s]", errno, strerror(errno));
				return 	FALSE;			
			}
		}
		BS_Tsyslog("Sync Queue request success(KILL)[%d].\n", curr_sync_thread - new_sync_thread);			
	}	

	// Async thread 개수 조절
	if (new_async_thread 		>	curr_async_thread)		// thread 증가
	{
		if (new_async_thread		>	MAX_ASYNC_THREAD_CNT)	// MAX값 초과한 경우
		{
			sprintf(msg_info->msg_text, "ASYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]", new_async_thread, MAX_ASYNC_THREAD_CNT,  MIN_ASYNC_THREAD_CNT);
			BS_Tsyslog("ASYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]\n", new_async_thread, MAX_ASYNC_THREAD_CNT,  MIN_ASYNC_THREAD_CNT);
			return FALSE;			
		}
		
		for (i = 0; i < MAX_ASYNC_THREAD_CNT; i++)
		{
			if ((async_thread[i].run_flag[0] == 'A') || (async_thread[i].run_flag[0] == 'S') || (async_thread[i].run_flag[0] == 'P'))
			{
				continue;
			}
		
			thr_id	=	pthread_create(&async_thread_array[i], NULL, async_processing, 		(void *) &i);
    		if (thr_id < 0)
    		{
    		    sprintf(msg_info->msg_text, "Async thread create error : [%d]th - [%d].", i, thr_id);
    		    BS_Tsyslog("Async thread create error : [%d]th - [%d].\n", i, thr_id);
				return FALSE;			    			    
    		}
			strcpy(async_thread[i].run_flag,    "Standby");		
			sprintf(async_thread[i].last_time,  "0000-00-00 00:00:00");
			sprintf(async_thread[i].last_time2, "00000000000000");
			async_thread[i].sec				=	0;
			async_thread[i].msec			=	0L;			
			memset(async_thread[i].msg_seq,	 0x00, sizeof(async_thread[i].msg_seq));
			memset(async_thread[i].msg_text, 0x00, sizeof(async_thread[i].msg_text));
    		// 식별번호 p_thread 를 가지는 쓰레드를 detach 
    		pthread_detach(async_thread_array[i]);
			sleep(1);

			BS_Tsyslog("Async thread add.  seq = [%d]\n", i);
			if (++curr_async_thread == new_async_thread)
			{
				break;	
			}
		}
	}
	if (new_async_thread 		<	curr_async_thread)		// thread 감소
	{
		if (new_async_thread		<	MIN_ASYNC_THREAD_CNT)	// MIN값 보다 작은 경우
		{
			sprintf(msg_info->msg_text, "ASYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]", new_async_thread, MAX_ASYNC_THREAD_CNT,  MIN_ASYNC_THREAD_CNT);
			BS_Tsyslog("ASYNC thread new value [%d] is not valid. Max=[%d]Min=[%d]\n", new_async_thread, MAX_ASYNC_THREAD_CNT,  MIN_ASYNC_THREAD_CNT);
			return FALSE;			
		}
		
		for (i = 0; i < (curr_async_thread - new_async_thread); i++)
		{
			// queue msg_type 0, msg_text "KILL"
			queue_id 			=	msgget(async_msg_key, IPC_CREAT | 0644);
			msg.msg_type		=	1L;
			strcpy(msg.msg_text, "KILL");
			result				=	msgsnd(queue_id, &msg, strlen(msg.msg_text), 0);
			if (result == -1)
			{
				BS_Tsyslog("Async Queue request failed(KILL), send ERROR %d, error message = [%s]\n", errno, strerror(errno));
				sprintf(msg_info->msg_text, "Async Queue request failed(KILL), send ERROR %d, error message = [%s]", errno, strerror(errno));
				return 	FALSE;			
			}
		}
		BS_Tsyslog("Async Queue request success(KILL)[%d].\n", curr_async_thread - new_async_thread);			
	}	

	// Mgt thread 개수 조절
	if (new_mgt_thread 		>	curr_mgt_thread)		// thread 증가
	{
		if (new_mgt_thread		>	MAX_MGT_THREAD_CNT)	// MAX값 초과한 경우
		{
			sprintf(msg_info->msg_text, "MGT thread new value [%d] is not valid. Max=[%d]Min=[%d]", new_mgt_thread, MAX_MGT_THREAD_CNT,  MIN_MGT_THREAD_CNT);
			BS_Tsyslog("MGT thread new value [%d] is not valid. Max=[%d]Min=[%d]\n", new_mgt_thread, MAX_MGT_THREAD_CNT,  MIN_MGT_THREAD_CNT);
			return FALSE;			
		}
		
		for (i = 0; i < MAX_MGT_THREAD_CNT; i++)
		{
			if ((mgt_thread[i].run_flag[0] == 'A') || (mgt_thread[i].run_flag[0] == 'S'))
			{
				continue;
			}
		
			thr_id	=	pthread_create(&mgt_thread_array[i], NULL, mgt_processing, 		(void *) &i);
    		if (thr_id < 0)
    		{
    		    sprintf(msg_info->msg_text, "Management thread create error : [%d]th - [%d].", i, thr_id);
    		    BS_Tsyslog("Management thread create error : [%d]th - [%d].\n", i, thr_id);
				return FALSE;			    			    
    		}
			strcpy(mgt_thread[i].run_flag,    "Standby");		
			sprintf(mgt_thread[i].last_time,  "0000-00-00 00:00:00");
			sprintf(mgt_thread[i].last_time2, "00000000000000");
			mgt_thread[i].sec				=	0;
			mgt_thread[i].msec			=	0L;			
			memset(mgt_thread[i].msg_text, 	0x00, sizeof(mgt_thread[i].msg_text));
    		// 식별번호 p_thread 를 가지는 쓰레드를 detach 
    		pthread_detach(mgt_thread_array[i]);
			sleep(1);

			BS_Tsyslog("Mgt thread add.  seq = [%d]\n", i);
			if (++curr_mgt_thread == new_mgt_thread)
			{
				break;	
			}
		}
	}
	if (new_mgt_thread 		<	curr_mgt_thread)		// thread 감소
	{
		if (new_mgt_thread		<	MIN_MGT_THREAD_CNT)	// MIN값 보다 작은 경우
		{
			sprintf(msg_info->msg_text, "MGT thread new value [%d] is not valid. Max=[%d]Min=[%d]", new_mgt_thread, MAX_MGT_THREAD_CNT,  MIN_MGT_THREAD_CNT);
			BS_Tsyslog("MGT thread new value [%d] is not valid. Max=[%d]Min=[%d]\n", new_mgt_thread, MAX_MGT_THREAD_CNT,  MIN_MGT_THREAD_CNT);
			return FALSE;			
		}
		
		for (i = 0; i < (curr_mgt_thread - new_mgt_thread); i++)
		{
			// queue msg_type 0, msg_text "KILL"
			queue_id 			=	msgget(mgt_msg_key, IPC_CREAT | 0644);
			msg.msg_type		=	1L;
			strcpy(msg.msg_text, "KILL");
			result				=	msgsnd(queue_id, &msg, strlen(msg.msg_text), 0);
			if (result == -1)
			{
				BS_Tsyslog("Management Queue request failed(KILL), send ERROR %d, error message = [%s]\n", errno, strerror(errno));
				sprintf(msg_info->msg_text, "Management Queue request failed(KILL), send ERROR %d, error message = [%s]", errno, strerror(errno));
				return 	FALSE;			
			}
		}
		BS_Tsyslog("Management Queue request success(KILL)[%d].\n", curr_mgt_thread - new_mgt_thread);			
	}	
	sprintf(msg_info->msg_text, "Thread Status : Sync=%d Async=%d Management=%d", new_sync_thread, new_async_thread, new_mgt_thread);
	return	TRUE;			

}

int		query_task(int thd_seq, BACKUP_INFO *msg_info)
{
	char			buffer[MAX_BUFFER_SIZE];
	char			search_text[MAX_BUFFER_SIZE];
	FILE			*fp;
	int				find;					// log key를 찾은 경우 flag.  0 : false, 1: true
	int				result;
	int				index;
	char			log_file[100];
	
	memset(buffer, 0x00, sizeof(buffer));
	msg_info->task_type	=	'9';		// default로 '9' 에러로 초기화
	strcpy(search_text, msg_info->msg_text);
	
	if (strlen(search_text) != 17)
	{
		sprintf(msg_info->msg_text, "TASK SEQ is not valid.[%s]", search_text);
		return FALSE;
	}
	sprintf(log_file, "%s-%.6s.log", TRAN_LOG_FILE, search_text);
    if ((fp = fopen(log_file, "r")) == NULL)
//    if ((fp = fopen(TRAN_LOG_FILE, "r")) == NULL)
    {
        BS_Tsyslog("Transaction Log file open error.[%s]\n", log_file);
        return FALSE;
    }
    else
    {
    	while(!feof(fp))
    	{
    	    fgets(buffer, MAX_LENGTH, fp);
    		if (buffer[0] != 0x00 || buffer[1] != '[')		// 로그 헤더가 아닌 데이터인경우
    		{
    			if (find == 0)								// 해당 SEQ가 아니면 skip
    			{
    				continue;
    			}
    			strcat(msg_info->msg_text, buffer);		// 해당 SEQ이고, 앞에 0x00이 없는 데이터이므로 전체 저장
//BS_Tsyslog("RESULT temp0 =[%c][%s]\n", msg_info->task_type, msg_info->msg_text);
    			continue;
    		}
			// printf("READ = %s\n", buffer);    				
    	    result = memcmp(search_text, &buffer[26], 17);
//BS_Tsyslog("READ ==> [%.*s][%.*s][%d]\n", 17, search_text, 17, &buffer[26], result);
			if (result < 0)			// 검색시간보다 로그 시간이 나중인 경우
    	    {
    	        // break;			// ASYNC인 경우 처리완료후 로깅되므로 로그 시간 순서가 일정하지 않게 되므로 파일 끝까지 검색하도록 변경 
    	        find  = 0;
				continue;
    	    }
			else if (result > 0)	// 검색시간보다 로그 시간이 이전인 경우
			{
    	    	find  = 0;
				continue;
			}
			find =  1;
			msg_info->task_type	=	buffer[44];
			memset(msg_info->msg_text, 0x00, sizeof(msg_info->msg_text));
    	    fgets(buffer, MAX_LENGTH, fp);
			// 일치하는 경우.  여러건이 있으므로 최종건까지 계속 검색하고, 기존 저장된 전문은 clear
// BS_Tsyslog("Message matched ==> [%.*s][%.*s][%d][%s]\n", 17, search_text, 17, &buffer[24], result, &buffer[1]);
    	    if (buffer[0] == 0x00)			// 결과 출력 전문 헤더인 경우
    	    {
    	    	if (buffer[1] == 'B')		// 앞에 전문 길이가 없는 일반 로그인 경우.  표준 전문 헤더 제외
    	    	{
    	    		strcat(msg_info->msg_text, &buffer[24]);	
    	    	}
    	    	else						// 앞에 전문 길이가 있는 일반 로그인 경우.  삭제된 로직이므로 디버깅 할 것 
    	    	{
    	    		strcat(msg_info->msg_text, &buffer[8]);
    	    	}
    	    }
    	    else							// 결과 출력이 한줄 이상인 경우
    	    {
    	    	strcat(msg_info->msg_text, buffer);
// BS_Tsyslog("RESULT temp1 =[%c][%s]\n", msg_info->task_type, msg_info->msg_text);
    	    }		
// BS_Tsyslog("RESULT temp2 =[%c][%s]\n", msg_info->task_type, msg_info->msg_text);
    	}
    }
    fclose(fp);
   
	// 장애처리 로직 추가
	// 시스템이 리부팅 되는 경우 메모리에는 없고, 로그 파일에만 결과가 있는 경우 발생
	// 처리 완료된 (정상, 오류등) 경우는 문제가 없으나 1,2,3 인 경우는 처리가 되지 않았으면서 status가 대기/작업중으로 조회됨
	// stauts를 7(mode 2)로 바꾸어 manager에서 수동으로 결과 확인하도록 처리함
	// Status 처리중으로 변경
//   	BS_Tsyslog("RESULT Before =[%c][%d][%s]\n", msg_info->task_type, index, msg_info->msg_text);
	if (msg_info->task_type == '1' || msg_info->task_type == '2' || msg_info->task_type == '3')
	{
		for (index = 0; index < MAX_MSG_CNT; index++)
		{
//   	BS_Tsyslog("RESULT Mid =[%c][%d][%s][%s][%s][%d]\n", msg_info->task_type, index, backup_info[index]->msg_seq, msg_info->msg_seq, search_text, sizeof(msg_info->msg_seq));
			// 처리중인 task이면서 shared memory에 있으면 정상
			if (memcmp(backup_info[index]->msg_seq, search_text, sizeof(msg_info->msg_seq)) == 0)
			{				
				break;
			}
		}
		if (index >= MAX_MSG_CNT)
		{
	    	BS_Tsyslog("[%.*s] Task already canceled.   Please check result.\n", 17, &msg_info->msg_seq);
//  		TRAN_LOG2('7', "Task cancel(mode 2)", "%s", &msg_info.msg_type);
    		msg_info->task_type	=	'7';
		}
	}	
BS_Tsyslog("[QUERY TRANSACTION] =[%c][%d][%s]\n", msg_info->task_type, index, msg_info->msg_text);

}
void *sync_processing(void *args)			// Sync type 전문 처리
{
	int				recv_queue_id;
	int				send_queue_id;
	int				msg_size;
	int				result;
	ssize_t			nbytes;
	BACKUP_INFO		msg_info;
	char			temp_file[100];

	char			buffer[MAX_BUFFER_SIZE];
	FILE			*fp;
	char			temp[MAX_BUFFER_SIZE];
	char			command[MAX_BUFFER_SIZE];
	int				thread_seq;
	Msg				msg;
	int				stime, etime;			// 시작/종료시간
	struct timeval  EndTime;
	int				i;
	int				timeout_value;
			
	recv_queue_id			=	msgget(sync_recv_msg_key, IPC_CREAT | 0644);
	send_queue_id			=	msgget(sync_send_msg_key, IPC_CREAT | 0644);
	
	thread_seq				=	*((int *)args);
//	thread_seq				=	(int) (int *)args;
	BS_Tsyslog("VBR Agent SYNC thread[%d]th start.\n", thread_seq);

	while (TRUE)
	{
//		nbytes 					=	msgrcv(queue_id, &msg, MAX_DATA_SIZE, 0, IPC_NOWAIT);
		memset(&msg, 0x00, sizeof(msg));
		memset(buffer, 0x00, sizeof(buffer));
		memset(&msg_info, 0x00, sizeof(msg_info));
		nbytes 					=	msgrcv(recv_queue_id, &msg, MAX_DATA_SIZE, -2100000000L, 0);
		gettimeofday(&sync_thread[thread_seq].start_time, NULL);
		
		if (nbytes > 0)
		{
			// thread kill msg이면 thread 종료
			if ((msg.msg_type	==	1L) && (memcmp(msg.msg_text, "KILL", strlen("KILL")) == 0))
			{
				break;
			}
			
			// printf("RECV msg : len = [%d]  queue-type = [%ld] data = [%s]\n", nbytes, msg.msg_type, msg.msg_text);
			TRAN_LOG2('3', "SYNC Queue receive", "%s", msg.msg_text);
			memcpy(&msg_info.msg_type, msg.msg_text, nbytes);

			// Status 처리중으로 변경
			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				if (memcmp(backup_info[i]->msg_seq, msg_info.msg_seq, sizeof(msg_info.msg_seq)) == 0)
				{
					backup_info[i]->status 	=	3;	
					break;
				}
			}
			if (i == MAX_MSG_CNT)
			{
BS_Tsyslog("************  원본 전문 검색 오류[%s]\n", 	msg_info.msg_seq);
			}

			// thread information update
			strcpy(sync_thread[thread_seq].run_flag, "Active");
			sprintf(sync_thread[thread_seq].msg_text, "%s", msg_info.msg_text);
			BS_date_time(sync_thread[thread_seq].last_time);
			BS_date_time_no_delimiter2(sync_thread[thread_seq].last_time2);

			memset(buffer,				0x00,	sizeof(buffer));
			memset(command,				0x00,	sizeof(command));
			// SYNC 처리는 timeout 값으로 alarm 처리
			memset(temp, 0x00, sizeof(temp));
			memcpy(temp, msg_info.timeout, sizeof(msg_info.timeout));
			timeout_value		=	atoi(temp);
			memset(command,	0x00,	sizeof(command));
			sprintf(command, "%s", msg_info.msg_text);
			memset(msg_info.msg_text,	0x00,	sizeof(msg_info.msg_text));
			dummy_call(sync_value + thread_seq, timeout_value, command);
			if (memcmp(command, "**TIMEOUT**", strlen("**TIMEOUT**")) == 0)
			{
				msg_info.task_type	=	'9';				// 실행 오류
				strcpy(msg_info.msg_text, "**TIMEOUT**");
			}
			else
			{
				msg_info.task_type	=	'0';				// 실행 종료
				strcat(msg_info.msg_text, command);
			}
			
			nbytes = 1+17+1+3+1+strlen(msg_info.msg_text);
			memset(msg.msg_text, 0x00, sizeof(msg.msg_text));
			memcpy(msg.msg_text, &msg_info.msg_type, nbytes);
			
			result				=	msgsnd(send_queue_id, &msg, nbytes, 0);
			if (result == -1)
			{
				BS_Tsyslog("SYNC Queue id %d, send ERROR %d\n", send_queue_id, errno);
				TRAN_LOG2('5', "SYNC Queue result send error", "%s", msg.msg_text);
			}
			else
			{
//				BS_Tsyslog("SYNC Queue id %d, send SUCCESS %s\n", send_queue_id, msg.msg_text);
				TRAN_LOG2('5', "SYNC Queue result send finished", "%s", msg.msg_text);
			}			
		}
		// Status 처리완료
		backup_info[i]->status 	=	5;	
		gettimeofday(&EndTime, NULL);
	    if (EndTime.tv_usec - sync_thread[thread_seq].start_time.tv_usec < 0)
	    {
	        EndTime.tv_sec--;
	        EndTime.tv_usec += 1000000;
	    }	
	    sync_thread[thread_seq].sec			=	(int)  EndTime.tv_sec  - sync_thread[thread_seq].start_time.tv_sec;
        sync_thread[thread_seq].msec		=	(long) EndTime.tv_usec - sync_thread[thread_seq].start_time.tv_usec;	
		strcpy(sync_thread[thread_seq].run_flag, 	"Standby");
//		BS_Tsyslog("SYNC [%d]th thread process. finished\n", thread_seq);
	}
	sync_thread[thread_seq].run_flag[0]  =  'N';
	BS_Tsyslog("SYNC [%d]th thread process is stop.\n", thread_seq);	
}

void *async_processing(void *args)			// Async type 전문 처리
{
	int				recv_queue_id;
	int				send_queue_id;
	int				msg_size;
	int				result;
	ssize_t			nbytes;
	BACKUP_INFO		msg_info;
	char			temp_file[100];

	char			buffer[MAX_BUFFER_SIZE];
	char			command[MAX_BUFFER_SIZE];
	FILE			*p;
	char			temp[MAX_BUFFER_SIZE];
	int				thread_seq;
	Msg				msg;
	int				stime, etime;			// 시작/종료시간
	struct timeval  StartTime, EndTime;
	int				i;
		
	recv_queue_id			=	msgget(async_recv_msg_key, IPC_CREAT | 0644);
	send_queue_id			=	msgget(async_send_msg_key, IPC_CREAT | 0644);
	
	thread_seq				=	*((int *)args);
//	thread_seq				=	(int) (int *)args;
	BS_Tsyslog("VBR Agent ASYNC thread[%d]th start.\n", thread_seq);

	while (TRUE)
	{
//		nbytes 					=	msgrcv(queue_id, &msg, MAX_DATA_SIZE, 0, IPC_NOWAIT);
		memset(&msg, 0x00, sizeof(msg));
		memset(buffer, 0x00, sizeof(buffer));
		memset(&msg_info, 0x00, sizeof(msg_info));


		// Xenserver, storage, cpu usage 상태를 확인해서 문제가 있는지 검증
		// async인 경우는 정상 인프라가 될때까지 무한 대기
		if ((health_check.xenserver[0] == 'A') && (health_check.storage[0] == 'A') && (health_check.cpu_usage < cpu_limit))
		{
			strcpy(async_thread[thread_seq].run_flag, "Standby");
		}
		else
		{
			strcpy(async_thread[thread_seq].run_flag, "Pending");
			sleep(3);
			continue;
		}

		nbytes 					=	msgrcv(recv_queue_id, &msg, MAX_DATA_SIZE, -2100000000L, 0);			
		// Xenserver, storage, cpu usage 상태를 확인해서 문제가 있는지 검증
		// async인 경우는 inqueue 처리후 정상 인프라가 될때까지 무한 대기
		// Queue receive 후에도 다시 한번 검증하도록.   정상 환경에서 queue 대기 상태였다가 나중에 장애 상황일 경우 대비
		if ((health_check.xenserver[0] == 'A') && (health_check.storage[0] == 'A') && (health_check.cpu_usage < cpu_limit))
		{
		}
		else
		{
			strcpy(async_thread[thread_seq].run_flag, "Pending");
			sleep(3);
			result				=	msgsnd(recv_queue_id, &msg, nbytes, 0);
			BS_Tsyslog("ASYNC Queue Waiting. queue id = [%ld] data = [%s] result = [%d].\n", msg.msg_type, msg.msg_text, result);
			continue;
		}
//		gettimeofday(&StartTime, NULL);
		gettimeofday(&async_thread[thread_seq].start_time, NULL);
	
		if (nbytes > 0)
		{
			// thread kill msg이면 thread 종료
			if ((msg.msg_type	==	1L) && (memcmp(msg.msg_text, "KILL", strlen("KILL")) == 0))
			{
				break;
			}

			// printf("RECV msg : len = [%d]  queue-type = [%ld] data = [%s]\n", nbytes, msg.msg_type, msg.msg_text);
			TRAN_LOG2('3', "ASYNC Queue receive", "%s", msg.msg_text);
			memcpy(&msg_info.msg_type, msg.msg_text, nbytes);

			// Status 처리중으로 변경
			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				if (memcmp(backup_info[i]->msg_seq, msg_info.msg_seq, sizeof(msg_info.msg_seq)) == 0)
				{
					backup_info[i]->status 	=	3;
					// 원본 전문을 찾은 경우만 msg sequence 정보 저장
					memcpy(async_thread[thread_seq].msg_seq, backup_info[i]->msg_seq, sizeof(async_thread[thread_seq].msg_seq));
//					BS_Tsyslog("ASYNC Msg Seq = [%.*s]\n", sizeof(async_thread[thread_seq].msg_seq), async_thread[thread_seq].msg_seq);
					break;
				}
			}
			if (i == MAX_MSG_CNT)
			{
BS_Tsyslog("************  원본 전문 검색 오류[%s]\n", 	msg_info.msg_seq);
			}

			// thread information update
			strcpy(async_thread[thread_seq].run_flag, "Active");
			sprintf(async_thread[thread_seq].msg_text, "%s", msg_info.msg_text);
			BS_date_time(async_thread[thread_seq].last_time);
			BS_date_time_no_delimiter2(async_thread[thread_seq].last_time2);

			// 백업 명령인 경우는 별도 처리
			if ((memcmp(msg_info.msg_text, "BACKUP",  strlen("BACKUP"))  == 0) ||
				(memcmp(msg_info.msg_text, "PARTIAL", strlen("PARTIAL")) == 0))
			{
				result	=	backup_vm(thread_seq, &msg_info);
				if (result	==	FALSE)
				{
					msg_info.task_type	=	'9';
				}
			}
			else if (memcmp(msg_info.msg_text, "RESTORE", strlen("RESTORE")) == 0)
			{
				result	=	import_vm(thread_seq, &msg_info);
				if (result	==	FALSE)
				{
					msg_info.task_type	=	'9';
				}
			}
			// 백업이 아닌 일반 명령인 경우 처리
			else
			{
				memset(command,	0x00,	sizeof(command));
				sprintf(command, "%s", msg_info.msg_text);
				memset(msg_info.msg_text,	0x00,	sizeof(msg_info.msg_text));
				dummy_call(async_value + thread_seq, 0, command);
				strcpy(msg_info.msg_text, command);
				msg_info.task_type	=	'0';
			}
			
			nbytes = 1+17+1+3+1+strlen(msg_info.msg_text);
			memset(msg.msg_text, 0x00, sizeof(msg.msg_text));			
			memcpy(msg.msg_text, &msg_info.msg_type, nbytes);
			
			result				=	msgsnd(send_queue_id, &msg, nbytes, 0);
			if (result == -1)
			{
				BS_Tsyslog("ASYNC Queue id %d, send ERROR %d\n", send_queue_id, errno);
				TRAN_LOG2('5', "ASYNC Queue result send error", "%s", msg.msg_text);
			}
			else
			{
//				BS_Tsyslog("ASYNC Queue id %d, send SUCCESS %s\n", send_queue_id, msg.msg_text);
				TRAN_LOG2('5', "ASYNC Queue result send finished", "%s", msg.msg_text);
			}
		}
		else
		{
			if (errno == ENOMSG)
			{
				BS_Tsyslog("Async Queue is empty.\n");
				continue;
			}
			else
			{
				BS_Tsyslog("Async Internal ERROR [%d].\n", errno);
				continue;
			}
		}
		// Status 처리완료
		backup_info[i]->status 	=	5;	
		gettimeofday(&EndTime, NULL);
	    if (EndTime.tv_usec - async_thread[thread_seq].start_time.tv_usec < 0)
	    {
	        EndTime.tv_sec--;
	        EndTime.tv_usec += 1000000;
	    }	
	    async_thread[thread_seq].sec		=	(int)  EndTime.tv_sec  - async_thread[thread_seq].start_time.tv_sec;
        async_thread[thread_seq].msec		=	(long) EndTime.tv_usec - async_thread[thread_seq].start_time.tv_usec;	
		strcpy(async_thread[thread_seq].run_flag, 	"Standby");
//		BS_Tsyslog("ASYNC [%d]th thread process. finished\n", thread_seq);
	}
	async_thread[thread_seq].run_flag[0]  =  'N';
	BS_Tsyslog("ASYNC [%d]th thread process is stop.\n", thread_seq);	
}

int backup_vm(int thd_seq, BACKUP_INFO *backup_msg)
{
/*
	BACKUP_INFO	*backup;
	backup	=	(BACKUP_INFO *)backup_msg;

	strcpy(backup_msg->msg_text, "BACKUP SUCCESS");
	backup_msg->task_type	=	'0';
	BS_Tsyslog("BACKUP : [%c][%s]\n", backup_msg->msg_type, backup_msg->msg_text);
*/
	FILE *p;
	char	buffer[MAX_BUFFER_SIZE];
	char	command[MAX_BUFFER_SIZE];
	char	vm_uuid[100];
	char	file_name[200];
	char	timestamp[20];
	char	snapshot_name[100];
	char	snapshot_uuid[40];
	int		result;
	char	disk_seq[MAX_VBD_CNT + 1];		// vm에 할당된 disk sequence no. bit
	int		backup_mode;

	if (memcmp(backup_msg->msg_text, "BACKUP",  strlen("BACKUP"))  == 0)
	{
		backup_mode		=	1;			// 일반 백업 mode
		sscanf(backup_msg->msg_text, "%s %s %s %s", buffer, vm_uuid, snapshot_name, file_name);
	}
	else if (memcmp(backup_msg->msg_text, "PARTIAL", strlen("PARTIAL")) == 0)
	{
		backup_mode		=	2;			// 부분 백업 mode
		sscanf(backup_msg->msg_text, "%s %s %s %s %s", buffer, vm_uuid, disk_seq, snapshot_name, file_name);		
	}
	else
	{
		sprintf(backup_msg->msg_text, "[ERROR]VBR COMMAND is not valid.");
		BS_Tsyslog("VBR COMMAND is not valid.[%s]\n", backup_msg->msg_text);
		return FALSE;			
	}
	
	if (strlen(vm_uuid) != 36)
	{
		strcpy(backup_msg->msg_text, "[ERROR]VM UUID is not valid.");
		BS_Tsyslog("VM UUID is not valid.[%s]\n", vm_uuid);
		return FALSE;
	}

	if (strlen(snapshot_name) < 1)
	{
		strcpy(backup_msg->msg_text, "[ERROR]Snapshot Name is not valid.");
		BS_Tsyslog("Snapshot Name is not valid.\n");
		return FALSE;
	}

	if (strlen(file_name) < 1)
	{
		strcpy(backup_msg->msg_text, "[ERROR]Export File Name is not valid.");
		BS_Tsyslog("Export File Name is not valid.\n");
		return FALSE;
	}
	
	if ((backup_mode == 2) && (strlen(disk_seq) != MAX_VBD_CNT))
	{
		strcpy(backup_msg->msg_text, "[ERROR]Partial Backup Disk sequence information is not valid.");
		BS_Tsyslog("Partial Backup Disk sequence information is not valid.[%d][%s]\n", strlen(disk_seq), disk_seq);
		return FALSE;
	}

	// VM snapshot
	memset(buffer,	0x00,	sizeof(buffer));
	memset(command,	0x00,	sizeof(command));
	BS_date_time_no_delimiter(timestamp);
	// snapshot 이름은 주어진 문자열을 그대로 사용하는 것으로 변경
	// strcat(snapshot_name, "-");
	// strcat(snapshot_name, timestamp);
	
//	sprintf(command, "./dummy 5 \"xe vm-snapshot uuid=%s new-name-label=%s\"", vm_uuid, timestamp);
//	sprintf(command, "xe vm-snapshot uuid=%s new-name-label=%s 2>&1", vm_uuid, snapshot_name);
//	BS_Tsyslog("Snapshot create Command : [%s]\n", command);

	memset(buffer,	0x00,	sizeof(buffer));
	sprintf(buffer, "xe vm-snapshot uuid=%s new-name-label=\\\"%s\\\"", vm_uuid, snapshot_name);
	memset(backup_msg->msg_text,	0x00,	sizeof(backup_msg->msg_text));
	dummy_call(async_value + thd_seq, 0, buffer);
	if (strlen(buffer) != 36)
	{
		BACKUP_LOG('S', vm_uuid, file_name, "VM Snapshot failed : [%d][%.*s]", strlen(buffer), strlen(buffer) - 1, buffer);
		BS_Tsyslog("VM Snapshot failed : [%d][%.*s]", strlen(buffer), strlen(buffer) - 1, buffer);
		backup_msg->task_type	=	'9';		
		strcpy(backup_msg->msg_text, "[ERROR]");
		strcat(backup_msg->msg_text, buffer);
		return FALSE;
	}
	else
	{
		memset(snapshot_uuid, 0x00, sizeof(snapshot_uuid));
		memcpy(snapshot_uuid, buffer, 36);		
		BACKUP_LOG('S', vm_uuid, file_name, "VM Snapshot success, uuid=%s", snapshot_uuid);
		// BS_Tsyslog("VM Snapshot success, uuid=%s", snapshot_uuid);
	}
	
	// Snapshot VM property change
//	memset(buffer,	0x00,	sizeof(buffer));
//	memset(command,	0x00,	sizeof(command));
//	sprintf(command, "xe template-param-set is-a-template=false ha-always-run=false uuid=%s 2>&1", snapshot_uuid);
//	sprintf(command, "./dummy 5 \"xe template-param-set is-a-template=false ha-always-run=false uuid=%s\"", snapshot_uuid);
//	BS_Tsyslog("Snapshot VM property change : [%s]\n", command);

	memset(buffer,	0x00,	sizeof(buffer));
	sprintf(buffer, "xe template-param-set is-a-template=false ha-always-run=false uuid=%s", snapshot_uuid);
	dummy_call(async_value + thd_seq, 0, buffer);
	if (strlen(buffer) > 0)			// 정상적인 경우 message가 없어야 함
	{
		BACKUP_LOG('P', snapshot_uuid, file_name, "VM Snapshot property change failed : [%d][%.*s]", strlen(buffer), strlen(buffer) - 1, buffer);
		BS_Tsyslog("VM Snapshot property change failed : [%d][%.*s]", strlen(buffer), strlen(buffer) - 1, buffer);
		backup_msg->task_type	=	'9';
		strcpy(backup_msg->msg_text, "[ERROR]");
		strcat(backup_msg->msg_text, buffer);
		remove_snapshot_vm(thd_seq, snapshot_uuid);
		return FALSE;
	}
	else
	{
		BACKUP_LOG('P', snapshot_uuid, file_name, "VM Snapshot property change, uuid=%s", snapshot_uuid);
	}

	// 부분 백업인 경우 백업대상이 아닌 snapshot의 VDI/VBD를 삭제후 백업해야 함
	if (backup_mode	==	2)
	{
		if (partial_backup_remove(async_value + thd_seq, snapshot_uuid, disk_seq) != TRUE)
		{
			BACKUP_LOG('R', snapshot_uuid, file_name, "Partial VM BACKUP (Remove VDI/VBD) failed.");
			BS_Tsyslog("Partial VM BACKUP (Remove VDI/VBD) failed.[%s][%s]", snapshot_uuid, file_name);
			backup_msg->task_type	=	'9';
			strcpy(backup_msg->msg_text, "[ERROR]");
			strcat(backup_msg->msg_text, "Partial VM BACKUP (Remove VDI/VBD) failed.");
			remove_snapshot_vm(thd_seq, snapshot_uuid);
			return FALSE;		
		}
	}
	// VM export
	memset(buffer,	0x00,	sizeof(buffer));
	memset(command,	0x00,	sizeof(command));
	sprintf(command, "rm -f %s", file_name);
	system(command);
//	BS_Tsyslog("file remove(if existed....[%s]", command);
//	memset(command,	0x00,	sizeof(command));
//	sprintf(command, "xe vm-export vm=%s filename=%s compress=true 2>&1", snapshot_uuid, file_name);
//	sprintf(command, "xe vm-export vm=%s filename=%s 2>&1", snapshot_uuid, file_name);
//	BS_Tsyslog("VM export : [%s]\n", command);
	memset(buffer,	0x00,	sizeof(buffer));
	sprintf(buffer, "xe vm-export vm=%s filename=\\\"%s\\\" compress=true", snapshot_uuid, file_name);
	dummy_call(async_value + thd_seq, 0, buffer);
	if (memcmp(buffer, "Export succeeded", strlen("Export succeeded")) != 0)			// 정상적인 경우 "Export succeeded"
	{
		BACKUP_LOG('E', snapshot_uuid, file_name, "Snapshot VM export failed : [%d][%.*s]", strlen(buffer), strlen(buffer) - 1, buffer);
		BS_Tsyslog("Snapshot VM export failed : [%d][%.*s]", strlen(buffer), strlen(buffer) - 1, buffer);
		backup_msg->task_type	=	'9';
		strcpy(backup_msg->msg_text, "[ERROR]");
		strcat(backup_msg->msg_text, buffer);
		remove_snapshot_vm(thd_seq, snapshot_uuid);
		return FALSE;
	}
	else
	{
		BACKUP_LOG('E', snapshot_uuid, file_name, "VM export succeeded.");
		chmod(file_name,  0666);
	}

	// Snapshot VM remove
	result = remove_snapshot_vm(thd_seq, snapshot_uuid);
	if (result == FALSE)			// Snapshot VM 삭제 오류인 경우
	{
		backup_msg->task_type	=	'9';
		sprintf(backup_msg->msg_text, "[ERROR]Remove Snapshot VM remove internal failed. Snapshot uuid=%s\n", snapshot_uuid);
		BS_Tsyslog("Remove Snapshot VM remove internal failed. Snapshot uuid=%s\n", snapshot_uuid);
		return FALSE;
	}
	else
	{
		sprintf(backup_msg->msg_text, "VM BACKUP Success : VM uuid=%s, Snapshot VM uuid=%s, backup filename=%s\n", vm_uuid, snapshot_uuid, file_name);
	}

	if (validate_xva(file_name) != TRUE) {
		backup_msg->task_type	=	'9';
		sprintf(backup_msg->msg_text, "[ERROR]XVA file = %s is not valid.\n", file_name);
		BS_Tsyslog("[ERROR]XVA file = %s is not valid.\n", file_name);
		return FALSE;		
	}
	else
	{
		sprintf(backup_msg->msg_text, "XVA file check Success : backup filename=%s\n", file_name);
	}

	backup_msg->task_type	=	'0';
	return TRUE;

//
}

int partial_backup_remove(int thd_seq, char *snapshot_uuid, char *disk_seq)
{
	FILE *fp, *fp2;
	char	buffer[MAX_BUFFER_SIZE];
	char	command[MAX_BUFFER_SIZE];
	char	buffer2[MAX_BUFFER_SIZE];
	char	command2[MAX_BUFFER_SIZE];
	char	vbd_list_text[100] 		= "uuid ( RO)    : ";
	int		i;
	int		result;

	typedef struct {
		char 	vdi_uuid[40];
		int 	userdevice;
		char	vbd_uuid[40];
		char	remove_flag;	// '0' : remove, '1' : backup
	} VBD_INFO;

	char	vdi_uuid[40];
	char	vbd_uuid[40];
	int		userdevice;
	
	VBD_INFO	vbd_info[MAX_VBD_CNT];

//BS_Tsyslog("Partial VM information check start.\n");
	memset(vbd_info, 0x00, sizeof(vbd_info));
	
	for (i = 0; i < MAX_VBD_CNT; i++)
	{
		vbd_info[i].remove_flag = *(disk_seq + i);
	}
//BS_Tsyslog("Partial VM information check 1.\n");
	
	i = 0;
//	while(fgets(buffer, sizeof(buffer), fp))
	// VBD list 검색
	memset(command,	0x00,	sizeof(command));
	sprintf(command, "xe vbd-list vm-uuid=%s type=Disk params=uuid", snapshot_uuid);
	dummy_call(async_value + thd_seq, 0, command);

	result = 0;
	while ((result = msg_cut(command, buffer)) == TRUE)
	{
//BS_Tsyslog("Partial VM information check [%s].\n", buffer);
		if (memcmp(buffer, vbd_list_text, strlen(vbd_list_text)) == 0)
		{
			// vbd uuid 저장
			memset(vbd_uuid, 0x00, sizeof(vbd_uuid));
			memcpy(vbd_uuid, &buffer[strlen(vbd_list_text)], 36);
			if (strlen(vbd_uuid) != 36)
			{
				BS_Tsyslog("VBD search internal ERROR. vbd_uuid information is not valid.[%s][%d]\n", vbd_uuid, strlen(vbd_uuid));
				return FALSE;
			}
			//BS_Tsyslog("***VBD [%s][%d]\n", vbd_uuid, strlen(vbd_uuid));
			//vbd_uuid[37]	=	0x00;
			//BS_Tsyslog("***VBD [%s][%d]\n", vbd_uuid, strlen(vbd_uuid));
			//vbd_uuid[strlen(vbd_uuid)-1] = 0x00;
			//BS_Tsyslog("***VBD [%s][%d]\n", vbd_uuid, strlen(vbd_uuid));
			// userdevice 검색
			memset(command2,	0x00,	sizeof(command2));
			sprintf(command2, "xe vbd-param-get uuid=%s param-name=userdevice", vbd_uuid);
			dummy_call(async_value + thd_seq, 0, command2);
			if (strlen(command2) <= 0)
			{
				BS_Tsyslog("VBD search internal ERROR. userdevice information is not valid.\n");
				return FALSE;
			}
			userdevice 						=	atoi(command2);
			vbd_info[userdevice].userdevice =	userdevice;
			
			// vdi uuid 검색
			memset(command2,	0x00,	sizeof(command2));
			sprintf(command2, "xe vbd-param-get uuid=%s param-name=vdi-uuid", vbd_uuid);
			dummy_call(async_value + thd_seq, 0, command2);
			strcpy(vdi_uuid, command2);
			if (strlen(vdi_uuid) != 36)
			{
				BS_Tsyslog("VBD search internal ERROR. vdi-uuid information is not valid.[%s][%s][%d][%s]\n", vbd_uuid, vdi_uuid, strlen(vdi_uuid), command);
				return FALSE;
			}
//			vdi_uuid[strlen(vdi_uuid)-1] = 0x00;
			
			strcpy(vbd_info[userdevice].vbd_uuid, vbd_uuid);
			vbd_info[userdevice].userdevice	=	userdevice;
			strcpy(vbd_info[userdevice].vdi_uuid, vdi_uuid);
//BS_Tsyslog("SNAPSHOT[%s], VBD[%s], USERDEVICE[%d], VDI[%s], FLAG[%c]\n", snapshot_uuid, vbd_uuid, userdevice, vdi_uuid, vbd_info[userdevice].remove_flag);
		}
		memset(buffer, 0x00, sizeof(buffer));
	} // while
	
	for (i = 0; i < MAX_VBD_CNT; i++)
	{
//BS_Tsyslog("DEBUG : [%s][%d][%c]\n", vbd_info[i].vbd_uuid, strlen(vbd_info[i].vbd_uuid),vbd_info[i].remove_flag);
		if ((strlen(vbd_info[i].vbd_uuid) == 36) && (vbd_info[i].remove_flag == '0'))
		{
			// vbd param setting
			memset(command,	0x00,	sizeof(command));
			sprintf(command, "xe vbd-param-set uuid=%s other-config:owner=false 2>&1", vbd_info[i].vbd_uuid);
			dummy_call(async_value + thd_seq, 0, command);
			if (strlen(command) > 1)
			{
				BS_Tsyslog("VBD param set ERROR. [%s][%s][%d]\n", vbd_info[i].vbd_uuid, command, strlen(command));
				return FALSE;
			}
			
			// vdi destroy
			memset(command,	0x00,	sizeof(command));
			sprintf(command, "xe vdi-destroy uuid=%s 2>&1", vbd_info[i].vdi_uuid);
			dummy_call(async_value + thd_seq, 0, command);
			if (strlen(command) > 1)
			{
				BS_Tsyslog("VDI Destroy ERROR. [%s][%s][%d]\n", vbd_info[i].vdi_uuid, command, strlen(command));
				return FALSE;
			}
			
			// vbd destroy
			// sprintf(command, "xe vbd-destroy uuid=%s 2>&1", vbd_info[i].vbd_uuid);
			// fp = popen(command, "r");
			// fgets(buffer, sizeof(buffer), fp);
			// pclose(fp);
			// if (strlen(buffer) > 0)
			// {
			// 	BS_Tsyslog("VDI Destroy ERROR. [%s][%s]\n", vbd_info[i].vdi_uuid, buffer);
			// 	pclose(fp);
			// 	return FALSE;
			// }
//			BS_Tsyslog("VDI destroy [%d][%s]\n", i, vbd_info[i].vdi_uuid);			
		}
	}

	return	TRUE;
}


// IMPORT /tmp/test.xva false d5946319-ab01-c5fc-a9a4-ceb73a043e45
int import_vm(int thd_seq, BACKUP_INFO *backup_msg)
{
	FILE *p;
	char	buffer[MAX_BUFFER_SIZE];
	char	command[MAX_BUFFER_SIZE];
	char	file_name[200];
	char	preserve[10];
	char	sr_uuid[40];
	int		result;
	
	memset(file_name, 		0x00,	sizeof(file_name));
	memset(preserve, 		0x00,	sizeof(preserve));
	memset(sr_uuid, 		0x00,	sizeof(sr_uuid));		
	sscanf(backup_msg->msg_text, "%s %s %s %s", buffer, file_name, preserve, sr_uuid);

	if (strlen(file_name) < 1)
	{
		strcpy(backup_msg->msg_text, "[ERROR]File Name is not valid.");
		return FALSE;
	}
	
	if (strlen(sr_uuid) != 36)
	{
		strcpy(backup_msg->msg_text, "[ERROR]SR UUID is not valid.");
		return FALSE;
	}

	if ((memcmp(preserve, "true", strlen("true")) != 0) && (memcmp(preserve, "false", strlen("false"))!= 0))
	{
		strcpy(backup_msg->msg_text, "[ERROR]Preserve option(true/false) is not valid.");
		return FALSE;
	}

	memset(buffer,	0x00,	sizeof(buffer));
	memset(command,	0x00,	sizeof(command));
	sprintf(command, "xe vm-import filename=\\\"%s\\\" sr-uuid=%s ", file_name, sr_uuid);

	if (memcmp(preserve, "true", strlen("true")) == 0)
	{
		strcat(command, "preserve=true 2>&1");
	}
	else
	{
		strcat(command, "preserve=false 2>&1");
	}

	dummy_call(async_value + thd_seq, 0, command);
	if (strlen(command) != 36)
	{
		IMPORT_LOG('I', sr_uuid, file_name, "VM import failed : [%d][%.*s]", strlen(command), strlen(command) - 1, command);
		backup_msg->task_type	=	'9';
		strcpy(backup_msg->msg_text, "[ERROR]");
		strcat(backup_msg->msg_text, command);
		return FALSE;
	}
	else
	{
//		command[37] 	=	0x00;
		strcpy(backup_msg->msg_text, command);		
		IMPORT_LOG('I', sr_uuid, file_name, "VM import success, vm-uuid=%s", backup_msg->msg_text);
	}	

	backup_msg->task_type	=	'0';
	return TRUE;

//
}

int	remove_snapshot_vm(int thd_seq, char *snapshot_uuid)
{
	FILE *p;
	char	buffer[MAX_BUFFER_SIZE];
	char	command[MAX_BUFFER_SIZE];
	char	vm_uuid[100];
	char	file_name[200];
	// Snapshot VM remove
	memset(buffer,			0x00,	sizeof(buffer));
	memset(command,			0x00,	sizeof(command));
//	sprintf(command, "./dummy 10 \"xe vm-uninstall uuid=%s force=true\"", snapshot_uuid);
	sprintf(command, "xe vm-uninstall uuid=%s force=true 2>&1", snapshot_uuid);

	memset(command,	0x00,	sizeof(command));
	sprintf(command, "xe vm-uninstall uuid=%s force=true 2>&1", snapshot_uuid);
	dummy_call(async_value + thd_seq, 0, command);
	if (memcmp(command, "Error:", strlen("Error:")) == 0)			// 정상적인 경우 다중 message가 display 됨
	{
		BACKUP_LOG('D', snapshot_uuid, file_name, "Snapshot VM remove failed : [%d][%.*s]", strlen(command), strlen(command) - 1, command);
		return FALSE;
	}
	else
	{
		BACKUP_LOG('D', snapshot_uuid, file_name, "Snapshot VM remove, uuid=%s,[%s]", snapshot_uuid, command);
		return TRUE;
	}
}

//////////////////////////////
void *backup_async_sender(void *args)
{
	Msg				msg;
	struct			msqid_ds	ds;
	key_t			async_msg_key		=	20005;				// 임시정의 (Async)
	int				queue_id;
	int				result;
	int				i;
	char 			temp[10];
	BACKUP_INFO		sync_info;
	int				send_len;
	ssize_t			nbytes;
	char			buffer[MAX_SEND_SIZE];
	
	queue_id 			=	msgget(async_msg_key, IPC_CREAT | 0644);

	BS_Tsyslog("Backup thread (ASYNC) start...\n");

		
	while (TRUE)
	{
		memset(&msg, 0x00, sizeof(msg));
		nbytes 					=	msgrcv(queue_id, &msg, MAX_DATA_SIZE, 0, 0);
		
		if (nbytes > 0)
		{
//			BS_Tsyslog("ASYNC Thread : RECV msg is : len = [%d]  queue-type = [%ld] data = [%s]\n", nbytes, msg.msg_type, msg.msg_text);
			memcpy(&sync_info.msg_type, msg.msg_text, nbytes);

			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				if (memcmp(backup_info[i]->msg_seq, sync_info.msg_seq, sizeof(sync_info.msg_seq)) == 0)							// 요청 전문 검색, 완료 처리, 응답 전문 송신등...
				{
					memset(buffer, 0x00, sizeof(buffer));
					sprintf(buffer, "%07d", nbytes);
					memcpy(&buffer[7], msg.msg_text, nbytes);
					send_len = write(backup_info[i]->fd, buffer, nbytes+7);
					if (send_len != nbytes+7)
   					{
						BS_Tsyslog("Send to REQUEST Manager internal error[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "ASYNC RESULT send error", "%s", &buffer[7]);
						backup_info[i]->status 		=	9;
					}
					else
					{
//						BS_Tsyslog("Send to REQUEST Manager 송신 완료[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "ASYNC RESULT send success", "%s", &buffer[7]);
						memset(&backup_info[i]->status, 0x00, sizeof(sync_info));
						backup_info[i]->status 		=	0;
					}
					break;
				}
			} // for
		}
		else
		{
			if (errno == ENOMSG)
			{
				BS_Tsyslog("Async Queue is empty..\n");
				continue;
			}
			else
			{
				BS_Tsyslog("Async Internal ERROR [%d]..\n", errno);
				continue;
			}
		}
		
		if (i >= MAX_MSG_CNT)
		{
			BS_Tsyslog("원본 요청 전문 검색 에러[%s]\n", msg.msg_text);
		}
	}	
}

void *backup_query_sender(void *args)
{
	Msg				msg;
	struct			msqid_ds	ds;
	key_t			mgt_msg_key		=	20009;				// 임시정의 (Query)
	int				queue_id;
	int				result;
	int				i;
	char 			temp[10];
	BACKUP_INFO		sync_info;
	int				send_len;
	ssize_t			nbytes;
	char			buffer[MAX_SEND_SIZE];
	int				len;
	
	queue_id 			=	msgget(mgt_msg_key, IPC_CREAT | 0644);

	BS_Tsyslog("Backup thread (Query) start...\n");

		
	while (TRUE)
	{
		memset(&msg, 0x00, sizeof(msg));
		nbytes 					=	msgrcv(queue_id, &msg, MAX_DATA_SIZE, 0, 0);
		
		if (nbytes > 0)
		{
//			BS_Tsyslog("QUERY Thread : RECV msg is : len = [%d]  queue-type = [%ld] data = [%s]\n", nbytes, msg.msg_type, msg.msg_text);
			memcpy(&sync_info.msg_type, msg.msg_text, nbytes);

			for (i = 0; i < MAX_MSG_CNT; i++)
			{
				if (memcmp(backup_info[i]->msg_seq, sync_info.msg_seq, sizeof(sync_info.msg_seq)) == 0)							// 요청 전문 검색, 완료 처리, 응답 전문 송신등...
				{
//					memset(buffer, 0x00, sizeof(buffer));
//					sprintf(buffer, "%07d", nbytes);
//					memcpy(&buffer[7], msg.msg_text, nbytes);
//					send_len = write(backup_info[i]->fd, buffer, nbytes+7);
//					len = strlen(sync_info.msg_text);
//					sprintf(buffer, "%07d", len);
//					memcpy(&buffer[7], sync_info.msg_text, len);
//					send_len = write(backup_info[i]->fd, buffer, len+7);
					memset(buffer, 0x00, sizeof(buffer));
					sprintf(buffer, "%07d", nbytes);
					memcpy(&buffer[7], msg.msg_text, nbytes);
					send_len = write(backup_info[i]->fd, buffer, nbytes+7);


					if (send_len != nbytes+7)
   					{
						BS_Tsyslog("Send to REQUEST Manager internal error[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "QUERY RESULT send error", "%s", &buffer[7]);
						backup_info[i]->status 		=	9;
					}
					else
					{
//						BS_Tsyslog("Send to REQUEST Manager 송신 완료[%d][%d][%s]\n", send_len, nbytes+7, msg.msg_text);
				        TRAN_LOG2('0', "QUERY RESULT send success", "%s", &buffer[7]);
						memset(&backup_info[i]->status, 0x00, sizeof(sync_info));
						backup_info[i]->status 		=	0;
					}
					break;
				}
			} // for
		}
		else
		{
			if (errno == ENOMSG)
			{
				BS_Tsyslog("MGT Query Queue is empty..\n");
				continue;
			}
			else
			{
				BS_Tsyslog("MGT Query Internal ERROR [%d].\n", errno);
				continue;
			}
		}
		
		if (i >= MAX_MSG_CNT)
		{
			BS_Tsyslog("원본 요청 전문 검색 에러[%s]\n", msg.msg_text);
		}
	}	
}

int	sequence_handler(int seq)				// unique queue id handler
{
	FILE	*fp;
	char	buffer[100];
	
	if (seq	== 1)		// initialize
	{
		if ((fp	=	fopen(SEQUENCE_FILE, "w")) == 0 )
    	{
    	    BS_Tsyslog("FATAL ERROR : Sequence FILE write open error [%s][%d].  STOP RUN.\n", SEQUENCE_FILE, errno);
    	    printf("FATAL ERROR : Sequence FILE write open error [%s][%d].  STOP RUN.\n", SEQUENCE_FILE, errno);
    	    exit(-1);
    	}
    	current_queue_seq	=	1L;
    	fclose(fp);
	}
	else           		// 기존 파일정보 + 1로 설정
	{
		if ((fp	=	fopen(SEQUENCE_FILE, "r")) == 0 )		// sequence file이 없는 최초 설정인 경우 1로 설정
    	{
    		if ((fp	=	fopen(SEQUENCE_FILE, "w")) == 0 )
    		{
    		    BS_Tsyslog("FATAL ERROR : Sequence FILE initialize open error [%s][%d].  STOP RUN.\n", SEQUENCE_FILE, errno);
    		    printf("FATAL ERROR : Sequence FILE initialize open error [%s][%d].  STOP RUN.\n", SEQUENCE_FILE, errno);
    		    exit(-1);
    		}    
    		current_queue_seq	=	1L;
    		fprintf(fp, "%ld", current_queue_seq);
        	fclose(fp);
			return TRUE;
    	}
    	else
    	{
        	fgets(buffer, MAX_LENGTH, fp);
        	current_queue_seq	=	atol(buffer) + 1L;
        	// BS_Tsyslog("Counter [%s][%ld]\n", buffer, current_queue_seq);
        	fclose(fp);
    		fp	=	fopen(SEQUENCE_FILE, "w");
    		{
    			fprintf(fp, "%ld", current_queue_seq);
 			   	fclose(fp);
    		}
    	}
    }  
}						

void signal_handler(int	sig)
{
	pid_t 			pid;
	int				rtn;

	switch(sig)	
	{
		case SIGCHLD : 
			pid		=	waitpid(-1,	&rtn, WNOHANG);
			break;
		default		 : BS_Tsyslog("Signal %d received.%d]\n", sig);
	}
}

int	event_register(char req_event_type, char *req_event_code, char *req_event_msg)
{
	int		i;
	int		result;
	// status => '0' : 초기화, '1' : 미송신, '9' : 송신 완료	
	/* disable 
	for (i = 0; i < MAX_EVENT_CNT; i++)
	{
		//동일한 경우는 overwrite
		if ((memcmp(event_msg[i].event_code, req_event_code, strlen(req_event_code)) == 0) &&
			(event_msg[i].status == '1'))
		{
			BS_date_time(event_msg[i].event_time);
//BS_Tsyslog("Time update[%d]\n", i);
			return	TRUE;
		}
		if (event_msg[i].status != '1')
		{
			event_msg[i].status			=	'1';
			event_msg[i].event_type		=	req_event_type;
			BS_date_time(event_msg[i].event_time);
			strcpy(event_msg[i].event_code, req_event_code);
			strcpy(event_msg[i].event_msg, 	req_event_msg);
//BS_Tsyslog("event insert[%d]\n", i);
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
	*/ // disable end
	
	// SK 20180512 로직 전체 변경 (미송신 전문 처리 오류)
	for (i = 0; i < MAX_EVENT_CNT; i++)
	{
		if (memcmp(event_msg[i].event_code, req_event_code, strlen(req_event_code)) == 0)
		{
			if (event_msg[i].status == '1')  		//미송신인 경우 시간만 update
			{
				BS_date_time(event_msg[i].event_time);
BS_Tsyslog("EVENT : Time update[%d][%s][%s]\n", i, req_event_code, req_event_msg);
				break;
			}
			else 
			{
				event_msg[i].event_type		=	req_event_type;
				BS_date_time(event_msg[i].event_time);
				strcpy(event_msg[i].event_code, req_event_code);
				strcpy(event_msg[i].event_msg, 	req_event_msg);
				event_msg[i].status			=	'1';
BS_Tsyslog("EVENT : Code reuse[%d][%s][%s]\n", i, req_event_code, req_event_msg);
				break;				
			}
		}
		if (strlen(event_msg[i].event_code) == 0)	//비어 있는 경우 event 신규 등록
		{
			event_msg[i].event_type		=	req_event_type;
			BS_date_time(event_msg[i].event_time);
			strcpy(event_msg[i].event_code, req_event_code);
			strcpy(event_msg[i].event_msg, 	req_event_msg);
			event_msg[i].status			=	'1';
BS_Tsyslog("EVENT : Code insert[%d][%s][%s]\n", i, req_event_code, req_event_msg);
			break;
		}
	}
	
	for (i = 0; i < MAX_EVENT_CNT; i++)
	{
		// 기존에 미송신한 전문까지 일괄 송신으로 처리하도록 변경
		if (event_msg[i].status == '1')
		{
			if ((result = send_portal(i)) == TRUE)
			{
				BS_Tsyslog("Event [%s][%s] Send to VBR WEB Portal success.\n", 	req_event_code, req_event_msg);
				event_msg[i].status			=	'9';
			}
			else
			{
				BS_Tsyslog("Event [%s][%s] Send to VBR WEB Portal failed.\n", 	req_event_code, req_event_msg);				
			}
		}
		else
		{
			break;
		}
	}	
	return FALSE;
}

int portal_initialize()
{
	char buffer[100];

	memset(buffer, 0x00, sizeof(buffer));
	// log file max size setup
	if (BS_GetProfileString("VBR Agent for Xenserver", "WEB server(IP)", buffer, sizeof(buffer)) == FALSE)
    {
    	portal_info	=	FALSE;
    	BS_Tsyslog("Warning. Config file[%s] [%s] parameter not found.\n", CONFIG_FILE, "WEB server(IP)");
    	return FALSE;
    }
    strcpy(portal_ip, buffer);
    
	if (BS_GetProfileString("VBR Agent for Xenserver", "WEB server(PORT)", buffer, sizeof(buffer)) == FALSE)
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

int	send_portal(int i)
{
	int 	errno;
	int		result;
	char	*ptr;
	char	command[1000];
	char	msg_type[100];
	char	tr_type[100];
	int 	sock = -1, sResult = -1;
	struct addrinfo hints, * res = NULL;
	char		temp[1000];
	char		send_data[5000];
	BACKUP_INFO	backup_msg;
	
	fd_set set;
	struct timeval timeout;
	int rv;


	if (portal_info != TRUE) return FALSE;

    memset(&hints, 0, sizeof(hints));
    hints.ai_family = PF_INET;
    hints.ai_socktype = SOCK_STREAM;
    sprintf(temp, "%d", portal_port);
    sResult = getaddrinfo(portal_ip, temp, &hints, &res);
    
    if (sResult < 0)
    {
        BS_Tsyslog("getaddrinfo() error %s %d\n", portal_ip, portal_port);
        return FALSE;
    }
			
    sock = socket(res->ai_family, res->ai_socktype, res->ai_protocol);
    if (sock == -1)
    {
        BS_Tsyslog("socket() error");
        return FALSE;
    }
    
    if (connect_with_timeout(sock, res, 5000) < 0)
    {
		BS_Tsyslog("WEB Portal Manager is not alive or check ip[%s], port[%d].\n", portal_ip, portal_port);
		return FALSE;
	}

	sprintf(temp, "{\"event_info\":[");
	strcpy(backup_msg.msg_text, temp);
	
	sprintf(temp, "{\"event_type\":\"%c\",\"host_name\":\"%s\",\"host_uuid\":\"%s\",\"event_time\":\"%s\",\"event_code\":\"%s\",\"event_msg\":\"%s\"}",
	                event_msg[i].event_type, g_host_name, g_host_uuid, event_msg[i].event_time, event_msg[i].event_code, event_msg[i].event_msg);
	strcat(backup_msg.msg_text, temp);
	strcat(backup_msg.msg_text, "]}");
	
	backup_msg.msg_type		=	'B';
	BS_date_time_no_delimiter2(backup_msg.msg_seq);
	strcat(backup_msg.msg_seq, "001");
	backup_msg.task_type	=	'0';
	memcpy(backup_msg.timeout, "000", sizeof(char)*3);
	backup_msg.priority		=	'1';
	
	memset(send_data, 0x00, sizeof(send_data));
	sprintf(send_data, "%07d", strlen((char *)&backup_msg.msg_type));
	memcpy(&send_data[7], &backup_msg.msg_type, strlen((char *)&backup_msg.msg_type));
	BS_Tsyslog("Before send to VBR WEB Portal manager[%s]\n", send_data);
	
    if (result = write(sock,&send_data, strlen(send_data)) < 0)     		
    {
		BS_Tsyslog("send to VBR WEB Portal manager => Failed.[%d][%s]\n", errno, send_data);
		return FALSE;
	}
	else
	{
		BS_Tsyslog("send to VBR WEB Portal manager => Success.[%s]\n", send_data);
	}

	FD_ZERO(&set); /* clear the set */
	FD_SET(sock, &set); /* add our file descriptor to the set */ 
	timeout.tv_sec = WAIT_TIME;
	timeout.tv_usec = 0;
	
	rv = select(sock + 1, &set, NULL, NULL, &timeout);
	if(rv == -1)
	{
		BS_Tsyslog("recv internal error\n");	/* an error accured */
		return FALSE;
	}	
	else if(rv == 0)
	{
		BS_Tsyslog("recv timeout error\n");	/* a timeout occured */
		return FALSE;
	}	
	else
	{			
		if (result = read(sock,&send_data, MAX_RECV_SIZE) < 0)
    	{
			BS_Tsyslog("recv from VBR WEB Portal manager => Failed.[%d]\n", errno);
			return FALSE;	
		}
		else
 		{
 			BS_Tsyslog("recv from VBR WEB Portal manager => Success.\n");
 			BS_Tsyslog("=====   RESULT   =====\n%s", send_data);
 			return TRUE;
 		}
 	}
}

// dummy process와 message queue 통신
int	dummy_call(long queue_value, int timeout, char *dummy_msg)
{
	Msg		msg;
	int		result, nbytes;
	FILE	*fp;
	char	command[MAX_BUFFER_SIZE];
	char	buffer[MAX_BUFFER_SIZE];

	sprintf(command, "./dummy2 %d %ld \"%s\"", timeout, queue_value, dummy_msg);
//BS_Tsyslog("COMMAND = [%s]\n", command);
	fp = popen(command, "r");

	memset(buffer, 0x00, sizeof(buffer));
	memset(&msg, 0x00, sizeof(msg));    
	fgets(buffer, sizeof(buffer), fp);
	pclose(fp);
	
	strcpy(dummy_msg, buffer);
		
	nbytes 	=	msgrcv(dummy_recv_queue_id, &msg, MAX_DATA_SIZE, queue_value, IPC_NOWAIT);

	if (queue_value != health_value)
	{
//BS_Tsyslog("SEND = [%s][%d], Recv = [%s]\n", command, nbytes, msg.msg_text);
	}
	if (nbytes >= 0)
	{
		strcpy(dummy_msg, msg.msg_text);
	}
	
/*
	if (queue_value != msg.msg_type)
	{
		BS_Tsyslog("MSG invalid. [%ld][%ld]\n", queue_value, msg.msg_type);
		return FALSE;	
	}
*/	
	if (strlen(dummy_msg) >= 1)
	{
		if (dummy_msg[strlen(dummy_msg)-1] == 0x0A)
		{
			*(dummy_msg+strlen(dummy_msg)-1) = 0x00;
		}
	}
//BS_Tsyslog("VBR Manager Execute result.[%ld][%s][%d]\n", queue_value, dummy_msg, strlen(dummy_msg));
	return TRUE;	
}

int connect_with_timeout(int sock, struct addrinfo * res, unsigned long timeout_milli)
{
    int err;
    socklen_t len;
    fd_set writefds;
    struct timeval timeout;
    int flags;
    FD_ZERO(&writefds);
    flags = fcntl(sock, F_GETFL);
    flags = (flags | O_NONBLOCK);
    if (fcntl(sock, F_SETFL, flags) != 0)
    {
        printf("fcntl() error\n");
        return -1;
    }
    if (connect(sock, res->ai_addr, res->ai_addrlen) != 0)
    {
        if (errno != EINPROGRESS)
        {
            printf("connect() error\n");
            return -1;
        }
    }
    timeout.tv_sec = timeout_milli / 1000;
    timeout.tv_usec = (timeout_milli % 1000) * 1000;
    FD_SET(sock, &writefds);
    if (select(sock+1, NULL, &writefds, NULL, &timeout) <= 0)
    {
        printf("connection timeout\n");
        return -1;
    }
    len = sizeof(err);
    getsockopt(sock, SOL_SOCKET, SO_ERROR, (char *)&err, &len);
    if (err)
    {
        printf("fcntl() error\n");
        return -1;
    }
    fcntl(sock, F_GETFL);
    flags = (flags & ~O_NONBLOCK);
    if (fcntl(sock, F_SETFL, flags) != 0)
    {
        printf("fcntl() error\n");
        return -1;
    }
    return 0;
}

// 파일이 gzip 압축되어 있는지 확인
int is_gzipped(const char *filename) {
    FILE *fp = fopen(filename, "rb");
    unsigned char magic[2];
    
    if (!fp) return 0;
    
    if (fread(magic, 1, 2, fp) != 2) {
        fclose(fp);
        return 0;
    }
    
    fclose(fp);
    return (magic[0] == 0x1f && magic[1] == 0x8b);
}

// 8진수 문자열을 long long으로 변환
long long octal_to_longlong(const char *octal_str, size_t len) {
    long long result = 0;
    size_t i;
    
    for (i = 0; i < len && octal_str[i] != '\0' && octal_str[i] != ' '; i++) {
        if (octal_str[i] >= '0' && octal_str[i] <= '7') {
            result = result * 8 + (octal_str[i] - '0');
        }
    }
    
    return result;
}

// TAR 헤더 체크섬 계산
unsigned int calculate_checksum(const tar_header_t *header) {
    unsigned int sum = 0;
    const unsigned char *ptr = (const unsigned char *)header;
    size_t i;
    
    for (i = 0; i < TAR_BLOCK_SIZE; i++) {
        if (i >= 148 && i < 156) {
            sum += ' ';
        } else {
            sum += ptr[i];
        }
    }
    
    return sum;
}

// 블록이 완전히 비어있는지 확인
int is_zero_block(const tar_header_t *header) {
    const unsigned char *ptr = (const unsigned char *)header;
    size_t i;
    
    for (i = 0; i < TAR_BLOCK_SIZE; i++) {
        if (ptr[i] != 0) {
            return 0;
        }
    }
    return 1;
}

// TAR 헤더 검증
int verify_tar_header(const tar_header_t *header) {
    unsigned int calculated_checksum, stored_checksum;
    
    if (is_zero_block(header)) {
        return 0;
    }
    
    if (header->name[0] == '\0') {
        return 0;
    }
    
    stored_checksum = octal_to_longlong(header->chksum, 8);
    if (stored_checksum == 0) {
        return 0;
    }
    
    calculated_checksum = calculate_checksum(header);
    
    return (stored_checksum == calculated_checksum);
}

// 파일명에서 디스크 블록 번호 추출
// 예: "Ref:148/00000010" -> 10
//     "Ref:148/00000010.xxhash" -> 10
long long extract_disk_block_number(const char *filename) {
    const char *ptr = strrchr(filename, '/');
    if (!ptr) return -1;
    
    ptr++;  // '/' 다음으로 이동
    
    // xxhash 확장자 제거
    char number_str[32] = {0};
    const char *dot = strchr(ptr, '.');
    if (dot) {
        size_t len = dot - ptr;
        if (len >= sizeof(number_str)) len = sizeof(number_str) - 1;
        strncpy(number_str, ptr, len);
    } else {
        strncpy(number_str, ptr, sizeof(number_str) - 1);
    }
    
    // 문자열을 숫자로 변환
    return strtoll(number_str, NULL, 10);
}

// 파일명이 xxhash인지 확인
int is_xxhash_file(const char *filename) {
    size_t len = strlen(filename);
    return (len > 7 && strcmp(filename + len - 7, ".xxhash") == 0);
}

// 압축된 XVA 파일 검증
int validate_compressed_xva(const char *filename, xva_validation_result_t *result) {
    gzFile gzfp;
    tar_header_t header;
    int empty_blocks = 0;
    long long total_bytes_read = 0;
    char prev_file_name[256] = {0};
    int unexpected_end = 0;
    
    result->is_compressed = 1;
    
    gzfp = gzopen(filename, "rb");
    if (!gzfp) {
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "gzip file open error: %s", strerror(errno));
        return -1;
    }
    
    // BS_Tsyslog("압축된 XVA 파일 검증 중...\n\n");
    
    while (1) {
        int bytes_read = gzread(gzfp, &header, TAR_BLOCK_SIZE);
        
        if (bytes_read < TAR_BLOCK_SIZE) {
            if (bytes_read == 0 || gzeof(gzfp)) {
                break;
            }
            // 읽기 오류 발생
            snprintf(result->error_msg, sizeof(result->error_msg), 
                    "gzip read error: unexpected file termination");
            unexpected_end = 1;
            break;
        }
        
        total_bytes_read += bytes_read;
        
        if (is_zero_block(&header)) {
            empty_blocks++;
            if (empty_blocks >= 2) {
                // BS_Tsyslog("\nTAR 아카이브 끝 감지 (정상 종료)\n");
                break;
            }
            continue;
        }
        
        if (!verify_tar_header(&header)) {
            empty_blocks++;
            if (empty_blocks >= 2) {
                break;
            }
            continue;
        }
        
        empty_blocks = 0;
        result->total_files++;
        
        // 현재 파일명 저장
        strncpy(prev_file_name, header.name, sizeof(prev_file_name) - 1);
        
        // 파일 타입 확인
        if (header.typeflag == '5') {
            result->directories++;
        } else if (header.typeflag == '0' || header.typeflag == '\0') {
            result->regular_files++;
        }
        
        long long file_size = octal_to_longlong(header.size, 12);
        
        /*
        if (result->total_files <= 20) {
            char type_char = header.typeflag;
            if (type_char == '\0') type_char = '0';
            
            BS_Tsyslog("[%3d] %-50s %10lld bytes [type:%c]", 
                   result->total_files, header.name, file_size, type_char);
            
            if (header.typeflag == '5') {
                BS_Tsyslog(" [DIR]");
            }
            
            if (file_size > 1024 * 1024) {
                BS_Tsyslog(" (%.2f MB)", file_size / (1024.0 * 1024.0));
            }
            BS_Tsyslog("\n");
        } else if (result->total_files == 21) {
            BS_Tsyslog("... (나머지 파일 생략) ...\n\n");
        }
        */
        
        if (strcmp(header.name, "ova.xml") == 0) {
            result->has_ova_xml = 1;
            // BS_Tsyslog("  ✓ ova.xml 발견!\n");
        }
        
        if (strncmp(header.name, "Ref:", 4) == 0) {
            result->disk_count++;
            
            // 마지막 파일 정보 업데이트
            strncpy(result->last_file_name, header.name, sizeof(result->last_file_name) - 1);
            
            // 블록 번호 추출
            long long block_num = extract_disk_block_number(header.name);
            if (block_num > result->last_disk_block_number) {
                result->last_disk_block_number = block_num;
            }
        }
        
        // 파일 데이터 건너뛰기
        long long blocks = (file_size + TAR_BLOCK_SIZE - 1) / TAR_BLOCK_SIZE;
        long long skip_bytes = blocks * TAR_BLOCK_SIZE;
        
        if (skip_bytes <= BUFFER_SIZE) {
            char *skip_buffer = malloc(skip_bytes);
            if (skip_buffer) {
                gzread(gzfp, skip_buffer, skip_bytes);
                free(skip_buffer);
            } else {
                gzseek(gzfp, skip_bytes, SEEK_CUR);
            }
        } else {
            gzseek(gzfp, skip_bytes, SEEK_CUR);
        }
        
        total_bytes_read += skip_bytes;
    }
    
    gzclose(gzfp);
    
    // 마지막 파일 검증
    result->last_file_is_xxhash = is_xxhash_file(result->last_file_name);
    
    /*
    BS_Tsyslog("\n압축 해제된 데이터: %lld 바이트 (%.2f GB)\n", 
           total_bytes_read, total_bytes_read / (1024.0 * 1024.0 * 1024.0));
    
    BS_Tsyslog("\n=== 마지막 파일 분석 ===\n");
    BS_Tsyslog("마지막 파일: %s\n", result->last_file_name);
    BS_Tsyslog("xxhash 파일 여부: %s\n", result->last_file_is_xxhash ? "예" : "아니오");
    BS_Tsyslog("마지막 블록 번호: %lld\n", result->last_disk_block_number);
    */
    
    // 예상치 못한 종료 확인
    if (unexpected_end) {
        result->validation_error = 1;
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "The file terminated unexpectedly. last file: %s", 
                prev_file_name);
        return -1;
    }
    
    // 마지막 파일이 xxhash가 아니면 오류
    if (!result->last_file_is_xxhash) {
        result->validation_error = 1;
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "The last file is not a .xxhash file: %s", 
                result->last_file_name);
        return -1;
    }
    
    // 디스크 용량 계산
    // 마지막 블록 번호가 N이면, 0부터 N까지 = (N+1)개 블록
    result->calculated_disk_size = (result->last_disk_block_number + 1) * DISK_BLOCK_SIZE;
    
    /*
    BS_Tsyslog("=== 디스크 용량 계산 ===\n");
    BS_Tsyslog("블록 수: %lld (0 ~ %lld)\n", 
           result->last_disk_block_number + 1, result->last_disk_block_number);
    BS_Tsyslog("블록 크기: %d 바이트 (1 MB)\n", DISK_BLOCK_SIZE);
    BS_Tsyslog("계산된 디스크 용량: %lld 바이트 (%.2f GB)\n", 
           result->calculated_disk_size, 
           result->calculated_disk_size / (1024.0 * 1024.0 * 1024.0));
    */
    
    if (result->total_files == 0) {
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "No valid TAR entry found");
        return -1;
    }
    
    result->is_valid = 1;
    return 0;
}

// 비압축 XVA 파일 검증
int validate_uncompressed_xva(const char *filename, xva_validation_result_t *result) {
    FILE *fp;
    tar_header_t header;
    int empty_blocks = 0;
    char prev_file_name[256] = {0};
    int unexpected_end = 0;
    
    result->is_compressed = 0;
    
    fp = fopen(filename, "rb");
    if (!fp) {
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "file open error: %s", strerror(errno));
        return -1;
    }
    
    // BS_Tsyslog("비압축 XVA 파일 검증 중...\n\n");
    
    while (fread(&header, TAR_BLOCK_SIZE, 1, fp) == 1) {
        if (is_zero_block(&header)) {
            empty_blocks++;
            if (empty_blocks >= 2) {
                // BS_Tsyslog("TAR 아카이브 끝 감지 (정상 종료)\n");
                break;
            }
            continue;
        }
        
        if (!verify_tar_header(&header)) {
            empty_blocks++;
            if (empty_blocks >= 2) {
                break;
            }
            continue;
        }
        
        empty_blocks = 0;
        result->total_files++;
        
        // 현재 파일명 저장
        strncpy(prev_file_name, header.name, sizeof(prev_file_name) - 1);
        
        // 파일 타입 확인
        if (header.typeflag == '5') {
            result->directories++;
        } else if (header.typeflag == '0' || header.typeflag == '\0') {
            result->regular_files++;
        }
        
        long long file_size = octal_to_longlong(header.size, 12);
        
        /*
        if (result->total_files <= 20) {
            char type_char = header.typeflag;
            if (type_char == '\0') type_char = '0';
            
            BS_Tsyslog("[%3d] %-50s %10lld bytes [type:%c]", 
                   result->total_files, header.name, file_size, type_char);
            
            if (header.typeflag == '5') {
                BS_Tsyslog(" [DIR]");
            }
            
            if (file_size > 1024 * 1024) {
                BS_Tsyslog(" (%.2f MB)", file_size / (1024.0 * 1024.0));
            }
            BS_Tsyslog("\n");
        } else if (result->total_files == 21) {
            BS_Tsyslog("... (나머지 파일 생략) ...\n\n");
        }
        
        */
        
        if (strcmp(header.name, "ova.xml") == 0) {
            result->has_ova_xml = 1;
            BS_Tsyslog("  ✓ ova.xml 발견!\n");
        }
        
        if (strncmp(header.name, "Ref:", 4) == 0) {
            result->disk_count++;
            
            // 마지막 파일 정보 업데이트
            strncpy(result->last_file_name, header.name, sizeof(result->last_file_name) - 1);
            
            // 블록 번호 추출
            long long block_num = extract_disk_block_number(header.name);
            if (block_num > result->last_disk_block_number) {
                result->last_disk_block_number = block_num;
            }
        }
        
        long long blocks = (file_size + TAR_BLOCK_SIZE - 1) / TAR_BLOCK_SIZE;
        if (fseek(fp, blocks * TAR_BLOCK_SIZE, SEEK_CUR) != 0) {
            snprintf(result->error_msg, sizeof(result->error_msg), 
                    "File navigation failure: Unexpected file termination");
            unexpected_end = 1;
            fclose(fp);
            return -1;
        }
    }
    
    fclose(fp);
    
    // 마지막 파일 검증
    result->last_file_is_xxhash = is_xxhash_file(result->last_file_name);
    
    /*
    BS_Tsyslog("\n=== 마지막 파일 분석 ===\n");
    BS_Tsyslog("마지막 파일: %s\n", result->last_file_name);
    BS_Tsyslog("xxhash 파일 여부: %s\n", result->last_file_is_xxhash ? "예" : "아니오");
    BS_Tsyslog("마지막 블록 번호: %lld\n", result->last_disk_block_number);
    */
    
    // 마지막 파일이 xxhash가 아니면 오류
    if (!result->last_file_is_xxhash) {
        result->validation_error = 1;
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "The last file is not a .xxhash file: %s", 
                result->last_file_name);
        return -1;
    }
    
    // 디스크 용량 계산
    result->calculated_disk_size = (result->last_disk_block_number + 1) * DISK_BLOCK_SIZE;
    
    /*
    BS_Tsyslog("\n=== 디스크 용량 계산 ===\n");
    BS_Tsyslog("블록 수: %lld (0 ~ %lld)\n", 
           result->last_disk_block_number + 1, result->last_disk_block_number);
    BS_Tsyslog("블록 크기: %d 바이트 (1 MB)\n", DISK_BLOCK_SIZE);
    BS_Tsyslog("계산된 디스크 용량: %lld 바이트 (%.2f GB)\n", 
           result->calculated_disk_size, 
           result->calculated_disk_size / (1024.0 * 1024.0 * 1024.0));
    */
    
    if (result->total_files == 0) {
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "No valid TAR entry found");
        return -1;
    }
    
    result->is_valid = 1;
    return 0;
}

// XVA 파일 검증 (압축 여부 자동 감지)
int open_and_validate_xva(const char *filename, xva_validation_result_t *result) {
    struct stat st;
    
    memset(result, 0, sizeof(xva_validation_result_t));
    
    if (stat(filename, &st) != 0) {
        snprintf(result->error_msg, sizeof(result->error_msg), 
                "file not found: %s", strerror(errno));
        return -1;
    }
    
    result->file_size = st.st_size;
    
    BS_Tsyslog("Start XVA file verification: %s\n", filename);
    BS_Tsyslog("file size: %lld 바이트 (%.2f GB)\n", result->file_size, result->file_size / (1024.0 * 1024.0 * 1024.0));
    
    // gzip 압축 여부 확인
    if (is_gzipped(filename)) {
        // BS_Tsyslog("파일 형식: gzip 압축된 TAR\n\n");
        return validate_compressed_xva(filename, result);
    } else {
        // BS_Tsyslog("파일 형식: 비압축 TAR\n\n");
        return validate_uncompressed_xva(filename, result);
    }
}

int validate_xva(char *xva_filename) {
    xva_validation_result_t result;
    
    
    BS_Tsyslog("===========================================\n");
    BS_Tsyslog("       XVA File Verification Tool\n");
    BS_Tsyslog("===========================================\n");
    
    if (open_and_validate_xva(xva_filename, &result) == 0) {
        BS_Tsyslog("===========================================\n");
        BS_Tsyslog("       ✓ Verification successful => %s\n", xva_filename);
        BS_Tsyslog("===========================================\n");
        BS_Tsyslog("file size: %lld bytes (%.2f GB)\n", 
               result.file_size, result.file_size / (1024.0 * 1024.0 * 1024.0));
        BS_Tsyslog("Compressed type: %s\n", result.is_compressed ? "Compressed (gzip)" : "uncompressed");
        // BS_Tsyslog("\n[파일 통계]\n");
        // BS_Tsyslog("  총 엔트리 수: %d\n", result.total_files);
        // BS_Tsyslog("  일반 파일:    %d\n", result.regular_files);
        // BS_Tsyslog("  디렉토리:     %d\n", result.directories);
        // BS_Tsyslog("  기타:         %d\n", result.total_files - result.regular_files - result.directories);
        // BS_Tsyslog("\n[XVA 구조]\n");
        // BS_Tsyslog("  ova.xml 존재: %s\n", result.has_ova_xml ? "예" : "아니오");
        // BS_Tsyslog("  디스크 관련 파일: %d\n", result.disk_count);
        // BS_Tsyslog("\n[디스크 정보]\n");
        // BS_Tsyslog("  마지막 파일: %s\n", result.last_file_name);
        // BS_Tsyslog("  마지막 블록 번호: %lld\n", result.last_disk_block_number);
        BS_Tsyslog("  Calculated VDI disk size: %lld byes (%.2f GB)\n", 
               result.calculated_disk_size,
               result.calculated_disk_size / (1024.0 * 1024.0 * 1024.0));
        BS_Tsyslog("===========================================\n");
        return TRUE;
    } else {
        BS_Tsyslog("===========================================\n");
        BS_Tsyslog("       ✗ Verification failed => %s\n", xva_filename);
        BS_Tsyslog("===========================================\n");
        BS_Tsyslog("Error: %s\n", result.error_msg);
        if (result.validation_error) {
            BS_Tsyslog("\n[Verification error details]\n");
            BS_Tsyslog("  Last detected file: %s\n", result.last_file_name);
            BS_Tsyslog("  The file may be damaged or incomplete\n");
        }
        BS_Tsyslog("===========================================\n");
        return FALSE;
    }
}
