#include	"bs_common.h"

void	signal_handler(int	sig);

typedef struct
{
	char 			msg_type;						// message type, Backup의 경우 'B'로 고정
	char			msg_seq[17];					// message sequence  YYYYMMDDHHMISS + 1/1000sec
	char			task_type;						// 송수신 전문 task 구분
	char			timeout[3];						// timeout value (sec, 0 : 무한 대기)
	char			priority;						// 처리 우선 순위 (1 : 즉시, 5 : 일반, 9 : 다른 task 우선 처리)
	char			msg_text[MAX_QUEUE_LEN];		// 원격 송신 자료, 처리 결과 자료
} BACKUP_INFO;

typedef struct {
	long 	msg_type;
	char 	msg_text[MAX_QUEUE_LEN];
} Msg;

int					timeout_flag		=	0;		// SIGNAL Alarm 처리를 위한 전역 변수
 	
int	main(int argc, char ** argv)
{
	Msg				msg;
	key_t			dummy_send_msg_key	=	30001;
	int				dummy_send_queue_id;
	int				msg_size;
	int				result;
	ssize_t			nbytes;
	BACKUP_INFO		backup_info;
	struct 			sigaction act;
		
	char			buffer[MAX_BUFFER_SIZE];
	FILE			*p, *pkill, *p2kill;		// p : 일반 pipe용, pkill : mq_sync에서 실행되는 상위 shell 확인, p2kill : 상위 shell에서 실행되는 하위 shell 확인
	char			temp[MAX_BUFFER_SIZE], temp2[MAX_BUFFER_SIZE];
	int				timeout_value;

	BS_initialize();
	// log file max size setup
	if (BS_GetProfileString("VBR Agent for Xenserver", "MAX Log size", buffer, sizeof(buffer)) == FALSE)
    {
    	max_log_file_size	=	50000000L;
    	BS_Tsyslog("FATAL Error. Config file[%s] [%s] parameter not found.(%ld default setting)..\n", CONFIG_FILE, "MAX Log size", max_log_file_size);
    }
    else
    {
    	max_log_file_size = atol(buffer);
//    	BS_Tsyslog("Log File Max size is [%ld]\n", max_log_file_size);
    }

	if	(argc != 4)
	{
		BS_Tsyslog("Dummy : input value is not valid....\n");
		exit(-1);
	}
	else
	{
//		BS_Tsyslog("Dummy : input value is [%s][%s][%s].\n", argv[1], argv[2], argv[3]);
	}
	
//	if (BS_Daemonize() == FALSE)
//	{
//		BS_Tsyslog("FATAL Error	[Remote Agent for Xenserver] STOP RUN(Daemonize).\n");
//		exit(-1);
//	}

	/* signal (alarm) 등록 */
	act.sa_handler	=	signal_handler;
	sigemptyset(&act.sa_mask);
	act.sa_flags	=	0;
	result			=	sigaction(SIGALRM, &act, 0); // 시그널 핸들러 등록
	// stderr message를 stdout message와 통합 관리
	
	if (result != 0) {
		BS_Tsyslog("SYNC manager : sigaction() error.  STOP RUN");
		exit(1);
	}
//	BS_Tsyslog("VBR Dummy agent start.\n");

	// dummy process와 통신을 위한 공통 queue 생성
	dummy_send_queue_id 			=	msgget(dummy_send_msg_key, IPC_CREAT | 0644);
		
	memset(buffer, 0x00, sizeof(buffer));
	memset(temp,   0x00, sizeof(temp));
	sprintf(buffer, "%s 2>&1", argv[3]);
	// BS_Tsyslog("Dummy : EXEC Command : [%s]\n", buffer);

	// SYNC 처리는 timeout 값으로 alarm 처리
	timeout_value		=	atoi(argv[1]);
	alarm(timeout_value);
//	BS_Tsyslog("DEBUG : %d sec limit Execute Begin [%s]\n", 	timeout_value, 	buffer);
	p = popen(buffer, "r");
	
//	BS_Tsyslog("Execute status code is [%d]\n", errno);

	msg.msg_type	=	atol(argv[2]);	
	while(fgets(buffer, sizeof(buffer), p)) {
	    // BS_Tsyslog("RESULT before : [%s]\n", buffer);				
		if (timeout_flag == -1)
		{
			break;
		}
		strcat(temp, buffer);
	    // BS_Tsyslog("RESULT : [%s]\n", buffer);				
	} // while
	// BS_Tsyslog("DEBUG : exit file loop....\n");
	alarm(0);

	if (timeout_flag == -1)
	{
		BS_Tsyslog("ERROR : %d초가 경과되어 명령 실행 중지를 시작합니다.[%s]\n", timeout_value, backup_info.msg_text);
		// 현재 실행중인 프로세스를 강제 종료하기 위한 script 실행 
		// 실행중인 shell은 종료되지만 OS에서 daemon으로 변환 실행되는 프로세스를 종료하기 위한 방안을 따로 설명해 둘 것
		// 아래는 shell 종료만 됨
		// sprintf(temp, "ps -ef|grep %s|grep -v grep |awk '{print $2}'|xargs kill -9", temp_file);
		
		while(TRUE)
		{
			// sprintf(temp, "pgrep -P %d|awk '{print $1}'|xargs pgrep -P|awk '{print $1}'|xargs kill -9", getpid());
			sprintf(temp, "pgrep -P %d|awk '{print $1}'|xargs echo|awk '{print $1}'", getpid());
//			BS_Tsyslog("DEBUG : kill command [%s]\n", temp);
			pkill = popen(temp, "r");			
	
			// pipe로 실행되는 상위 process 하나만 select
			memset(buffer, 0x00, sizeof(buffer));
			if (fgets(buffer, sizeof(buffer), pkill) == NULL || (strlen(trim(temp)) == 0))
			{
				// 더이상 종료할 하위 shell process가 없음
				pclose(pkill);
				break;
			}
			
			// 종료할 client process가 있는 경우
			while (TRUE)
			{
//				BS_Tsyslog("하위 shell pid [%s]\n", trim(buffer));
				sprintf(temp, "pgrep -P %s|awk '{print $1}'|xargs echo|awk '{print $1}'", trim(buffer));
//				BS_Tsyslog("DEBUG : sub kill command [%s]\n", temp);
				p2kill = popen(temp, "r");			
	        	
				// pipe로 실행되는 하위 process 하나만 select
				if (fgets(temp, sizeof(temp), p2kill) == NULL || (strlen(trim(temp)) == 0))
				{
					// 더이상 종료할 하위 shell process가 없음
					pclose(p2kill);
					break;
				}
				sprintf(temp2, "kill -9 %s", trim(temp));
//				BS_Tsyslog("DEBUG : kill system command [%s]\n", temp2);
				system(temp2); sleep(1);
				pclose(p2kill);						
			}
			
			pclose(pkill);
			break;		// 임시로 한번만 loop 돌도록		
		}
		
//		BS_Tsyslog("KILL Process [%s]\n", temp);
		
		BS_Tsyslog("Dummy ERROR : %d초가 경과되어 명령 실행이 중지되었습니다.[%s][%s]\n",atoi(argv[1]), argv[2], argv[3]);
		printf("%s", "**TIMEOUT**");
		strcpy(msg.msg_text, "**TIMEOUT**");
		msg.msg_type	=	atol(argv[2]);
	}
	else
	{
		// BS_Tsyslog("Dummy execute success.[%d][%s][%s]\n[%s]\n", atoi(argv[1]), argv[2], argv[3], temp);
		printf("%s", temp);
		strcpy(msg.msg_text, temp);
		msg.msg_type	=	atol(argv[2]);
		fflush(stdout);
		fflush(stderr);
	}
	fflush(stdout);
	fflush(stderr);
	pclose(p);

//	BS_Tsyslog("Before send queue.  [%ld][%s]\n", strlen(msg.msg_text), msg.msg_text);
//	BS_Tsyslog("Detail info: input value = [%s][%s][%s], Queue info = [%ld][%ld][%s]\n", argv[1], argv[2], argv[3], msg.msg_type, strlen(msg.msg_text), msg.msg_text);
	result			=	msgsnd(dummy_send_queue_id, &msg, strlen(msg.msg_text), 0);
//	BS_Tsyslog("After send queue.  [%ld][%s]\n", strlen(msg.msg_text), msg.msg_text);
	if (result == -1)
	{
		BS_Tsyslog("Dummy dispacher queue send ERROR [%d][%s]\n", errno, strerror(errno));
		BS_Tsyslog("Detail info: input value = [%s][%s][%s], Queue info = [%ld][%ld][%s]\n", argv[1], argv[2], argv[3], msg.msg_type, strlen(msg.msg_text), msg.msg_text);
		return 	FALSE;
	}
	return TRUE;
}

void signal_handler(int	sig)
{
	pid_t	pid;
	int rtn;

	switch(sig) {
		case SIGALRM :
			BS_Tsyslog("*************** Dummy Manager : Alarm Signal received.\n");
			timeout_flag	=	-1;
			alarm(0);
			break;
		default	: BS_Tsyslog("SYNC Manager : Signal received .%d]\n", sig);
	}
 
}
