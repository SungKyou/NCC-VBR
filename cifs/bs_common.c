#include	"bs_common.h"
#include	<sys/time.h>
#include	<sys/ioctl.h>

extern int BS_SubLog(const char *pp_File, const char *pp_Function, const int pi_Line, 
              struct timeval *pt_StartTime, struct timeval *pt_EndTime, int pi_RValue)
/**************************************************************************************************/
/* DESC    : sublog (향후 Std SYSLOG와 연동될 임시 module)                                        */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    if (pi_RValue == -1)        /* Start_Func에서 call 되는 경우 */
    {
        gettimeofday((struct timeval *) pt_StartTime, NULL);
        BS_Tsyslog("[%s|%s|%d|Start.\n", pp_File, pp_Function, pi_Line);
        return TRUE;
    }
    
    gettimeofday((struct timeval *) pt_EndTime, NULL);
    if (pt_EndTime->tv_usec - pt_StartTime->tv_usec < 0)
    {
        pt_EndTime->tv_sec--;
        pt_EndTime->tv_usec += 1000000;
    }
    BS_Tsyslog("[%s|%s|%d|End(%d.%06ld초 소요).\n", pp_File, pp_Function, pi_Line, 
                                        (int) pt_EndTime->tv_sec - pt_StartTime->tv_sec, 
                                        pt_EndTime->tv_usec - pt_StartTime->tv_usec);
    return pi_RValue;
}

extern int BS_NetworkListen(char *pp_Session, char *pp_Key)
/**************************************************************************************************/
/* DESC    : BS_NetworkListen                                                                     */
/* IN_ARG  : session : config file session value                                                  */
/*           key     : config file search key value                                               */
/* OUT_ARG : si_PortNo (listen port no)                                                           */
/*           si_ServSock (listen socket fd)                                                       */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    int		li_On, buff_size;
    struct	sockaddr_in lt_ServAddr;
    char	lx_Value[MAX_LENGTH];
    FILE	*fp;
    char	buffer[1000];
    char 	command[1000];
    int		i;
    int		val;

    if (BS_GetProfileString(pp_Session, pp_Key, lx_Value, sizeof(lx_Value)) == FALSE)
    {
        BS_Tsyslog("FATAL Error. Config file[%s] TCP/IP port value is invalid.\n", CONFIG_FILE);
        BS_Tsyslog("STOP RUN.\n");
        exit(-1);
    }
    si_PortNo = atoi(lx_Value);
    BS_Tsyslog("Network listen starting : portno = [%d]\n", si_PortNo);

    if ((si_ServSock = socket(AF_INET, SOCK_STREAM, 0)) < 0) 
    {
        BS_Tsyslog("socket init error: [%d]\n", errno);
        return FALSE;
    }

	// server socket 상속되지 않도록 close
	val = fcntl(si_ServSock, F_GETFD, 0);
    // FD_CLOEXEC 를 세팅한다. 
    val |= FD_CLOEXEC;
    if (val & FD_CLOEXEC)
        BS_Tsyslog("server socket close-on-exec bit on\n");
    else
        BS_Tsyslog("server socket close-on-exec bit off\n");

    li_On = 1;
    if (setsockopt(si_ServSock, SOL_SOCKET, SO_REUSEADDR, (char *) &li_On, sizeof(li_On)) < 0) 
    {
        BS_Tsyslog("setsockopt error: SO_REUSEADDR [%d]\n", errno);
    }

    buff_size = 100000;    
    if (setsockopt(si_ServSock, SOL_SOCKET, SO_RCVBUF, (void *) &buff_size, sizeof(li_On)) < 0) 
    {
        BS_Tsyslog("setsockopt error: SO_RCVBUF [%d]\n", buff_size);
    }

    if (getsockopt(si_ServSock, SOL_SOCKET, SO_RCVBUF, &buff_size, (socklen_t *) &li_On) < 0) 
    {
        BS_Tsyslog("getsockopt error: SO_RCVBUF [%d]\n", buff_size);
    }
    BS_Tsyslog("getsockopt : SO_RCVBUF [%d]\n", buff_size);

    memset((char *)&lt_ServAddr, 0, sizeof(lt_ServAddr));
    lt_ServAddr.sin_family      = AF_INET;
    lt_ServAddr.sin_addr.s_addr = htonl(INADDR_ANY);
    lt_ServAddr.sin_port        = htons((unsigned short)si_PortNo);

	// bind 오류를 방지하기 위해 port를 점유하고 있는 child 프로세스를 강제 종료.  단, 이중 실행을 대비 process daemon이 실행중이면 종료하도록 flag(EXIST) 처리 
	if (BS_GetProfileString(pp_Session, "PROGRAM", lx_Value, sizeof(lx_Value)) == FALSE)
	{
        BS_Tsyslog("FATAL Error. Config file[%s] Daemon program value is invalid.\n", CONFIG_FILE);
    	BS_Tsyslog("STOP RUN.\n");
    	exit(-1);
    }
	
	memset(buffer, 0x00, sizeof(buffer));
	sprintf(command, "ps -ef|grep %s|grep -v grep|wc -l", lx_Value);    
	fp = popen(command, "r");
	fgets(buffer, sizeof(buffer), fp);
	if (atoi(buffer) > 1)
	{
		BS_Tsyslog("DAEMON process [%s][%.1s] is already running.\n", lx_Value, buffer);
		return EXIST;
	}
	
	// port를 점유하는 child process 강제 종료 (loop 3번) 
    for (i = 0; i < 3; i++)
    {
    	if (bind(si_ServSock, (struct sockaddr *)&lt_ServAddr, sizeof(lt_ServAddr)) < 0)
    	{
			sprintf(command, "fuser -k -n tcp %d", si_PortNo);
			system(command);
      	    BS_Tsyslog("BIND error. Kill process[%d]th. [%d] port Process.\n", i, si_PortNo);
    	}
    	else
    	{
    		break;
    	}
    }

	// 프로세스가 죽지 않는 경우 에러 처리
	if (i >= 3)
	{
   	    BS_Tsyslog("BIND error. [%d] port used Process is still running.\n", i, si_PortNo);		
   	    close(si_ServSock);
  	    return FALSE;	
	}
	
    if (listen(si_ServSock, 5) < 0)
    {
        BS_Tsyslog("Listen internal error[%d].\n", errno);
        close(si_ServSock);
        return FALSE;
    }
    return TRUE;
}

extern int BS_Connect(char *server, int *pp_Fd)
/**************************************************************************************************/
/* DESC    : BS_Connect                                                                    		  */
/* IN_ARG  : config file title name ex(cloud manager, provisioning, ...)						  */ 
/* OUT_ARG : fd(client socket fd)                                                                 */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    int     li_ClntSock;
    int		li_PortNo;
    struct hostent  *lt_Hp;
    struct sockaddr_in lt_ServAddr, lt_InAddr;
    char lx_Value[MAX_LENGTH];

    if (BS_GetProfileString(server, "PORT-NO", lx_Value, sizeof(lx_Value)) == FALSE)
    {
        BS_Tsyslog("FATAL Error. Config file[%s] [%s] port no not found...\n", CONFIG_FILE, server);
		return FALSE;
    }
    li_PortNo = atoi(lx_Value);

    if (BS_GetProfileString(server, "SERVER-IP", lx_Value, sizeof(lx_Value)) == FALSE)
    {
        BS_Tsyslog("FATAL Error. Config file[%s] [%s] ip address no not found...\n", CONFIG_FILE, server);
		return FALSE;
    }

    BS_Tsyslog("Client network_connect: host = [%s] portno = [%d]", lx_Value, li_PortNo);

    memset((char *)&lt_ServAddr, 0, sizeof(lt_ServAddr));
    lt_ServAddr.sin_family = AF_INET;
    lt_ServAddr.sin_port   = htons((unsigned short)li_PortNo);

/*      memset(&serv_addr, 0, sizeof(serv_addr));
        serv_addr.sin_family=AF_INET;
        serv_addr.sin_addr.s_addr=inet_addr(argv[2]);
        serv_addr.sin_port=htons(atoi(argv[3]));
*/
    if (BS_GetIpByName(lx_Value, &lt_InAddr) == TRUE) 
    {
        BS_Tsyslog("Getipbyname find success[%s]\n", lx_Value);
        memcpy(&lt_ServAddr.sin_addr, &lt_InAddr, sizeof(struct in_addr));
    }
    else 
    {
        BS_Tsyslog("Connect parameter check : [%s][%d]", lx_Value, errno);
        return FALSE;
    }

    if ((li_ClntSock = socket(AF_INET, SOCK_STREAM, 0)) < 0) 
    {
        BS_Tsyslog("Network connect socket error: [%d]\n", errno);
        return FALSE;
    }
    lt_ServAddr.sin_addr.s_addr=lt_InAddr.sin_addr.s_addr;
    if (connect(li_ClntSock, (struct sockaddr *)&lt_ServAddr, sizeof(lt_ServAddr)) >= 0)
    {
        *pp_Fd = li_ClntSock;   
        BS_Tsyslog("Network connect success: [%s][%s][%d]==>[%d]\n", server, lx_Value, li_PortNo, errno);
        return TRUE;
    }
    else
    {
        close(li_ClntSock);
        BS_Tsyslog("Network connect error: [%s][%s][%d]==>[%d]\n", server, lx_Value, li_PortNo, errno);
        return FALSE;
    }
}

extern int BS_NetworkAccept(char *ipaddress)
/**************************************************************************************************/
/* DESC    : BS_NetworkAccept                                                                     */
/* IN_ARG  : si_ServSock(listen socket fd)                                                          */
/* OUT_ARG : si_ClntSock (accept socket fd)                                                                */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    int     li_Len;
    struct sockaddr_in lt_ClntAddr;

    li_Len = sizeof(lt_ClntAddr);
    /* For AIX Only */
    si_ClntSock = accept(si_ServSock, (struct sockaddr *)&lt_ClntAddr, (socklen_t *)&li_Len);
    if (si_ClntSock < 0)
    {
        BS_Tsyslog("Accept client socket internal failed.[%d][%d]\n", si_ClntSock, errno);
        return FALSE;
    }
    
    XD_Tsyslog("Client Connecting: ip = [%d][%s]\n", si_ClntSock, inet_ntoa(lt_ClntAddr.sin_addr));
    sprintf(ipaddress, "%s", inet_ntoa(lt_ClntAddr.sin_addr));
    
    BS_Tsyslog("New connection received: ip = [%d][%s]\n", si_ClntSock, ipaddress);
    return TRUE;
}

extern int BS_NetworkConnect(char *pp_Host, unsigned int pi_Ip, int pi_PortNo, int *pp_Fd)
/**************************************************************************************************/
/* DESC    : BS_NetworkConnect                                                                    */
/* IN_ARG  : host(접속할 hostname 또는 xxx.xxx.xxx.xxx, ip로 접속되는 경우 NULL setting)          */
/*         : ip (접속할 server ip address, hostname setting 되는 경우 0 setting)                  */
/*         : portno (접속할 port no)                                                              */
/* OUT_ARG : fd(client socket fd)                                                                 */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    int     li_ClntSock;
    struct hostent  *lt_Hp;
    struct sockaddr_in lt_ServAddr, lt_InAddr;

    BS_Tsyslog("network_connect: host = [%s] portno = [%d]", pp_Host, pi_PortNo);

    memset((char *)&lt_ServAddr, 0, sizeof(lt_ServAddr));
    lt_ServAddr.sin_family = AF_INET;
    lt_ServAddr.sin_port   = htons((unsigned short)pi_PortNo);

/*      memset(&serv_addr, 0, sizeof(serv_addr));
        serv_addr.sin_family=AF_INET;
        serv_addr.sin_addr.s_addr=inet_addr(argv[2]);
        serv_addr.sin_port=htons(atoi(argv[3]));
*/
    if ((pi_Ip != INADDR_NONE) && (pi_Ip != 0)) 
    {
        BS_Tsyslog("IP address processing\n");
        memcpy((char *)&lt_ServAddr.sin_addr, (char *)&pi_Ip, sizeof(struct in_addr));
    }
    else if (BS_GetIpByName(pp_Host, &lt_InAddr) == TRUE) 
    {
        BS_Tsyslog("Getipbyname find success\n");
        memcpy(&lt_ServAddr.sin_addr, &lt_InAddr, sizeof(struct in_addr));
    }
    else 
    {
        BS_Tsyslog("Connect parameter check : [%d]", errno);
        return FALSE;
    }

    if ((li_ClntSock = socket(AF_INET, SOCK_STREAM, 0)) < 0) 
    {
        BS_Tsyslog("Network connect socket error: [%d]\n", errno);
        return FALSE;
    }
    lt_ServAddr.sin_addr.s_addr=lt_InAddr.sin_addr.s_addr;
    if (connect(li_ClntSock, (struct sockaddr *)&lt_ServAddr, sizeof(lt_ServAddr)) >= 0)
    {
        *pp_Fd = li_ClntSock;   
        return TRUE;
    }
    else
    {
        close(li_ClntSock);
        BS_Tsyslog("Network connect error: [%d]\n", errno);
        return FALSE;
    }
}

extern int BS_NetworkReadN(int pi_Fd, char *pp_Ptr, int pi_Nbytes, int *pi_Nrecv)
/**************************************************************************************************/
/* DESC    : BS_NetworkReadN                                                                      */
/* IN_ARG  : fd : read socket fd                                                                  */
/*         : ptr : received buffer                                                                */
/*         : nbytes : read 할 전체 length                                                         */
/* OUT_ARG : nrecv : received length                                                              */
/*                   nbytes = nrecv : 정상                                                        */
/*                   nbytes > nrecv : 오류 (수신중 오류발생, buffer full or socket close)         */
/*                   nrecv  = 0     : 오류 (socket close)                                         */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    int li_N, li_Err, li_Nleft;
    int li_Nread, li_Retry;

    li_Retry = 0;
    li_Nleft = pi_Nbytes;
    *pi_Nrecv = 0;          /* default return value : 0 setting, 오류일때만 0으로 return */
    while(li_Nleft > 0) 
    {
        li_Nread = recv(pi_Fd, pp_Ptr, li_Nleft, 0);
        if (li_Nread < 0) 
        {
            if ((errno == EWOULDBLOCK) || (errno == EAGAIN)) 
            {
                li_Retry++;
                if (li_Retry > 50)
                {
                    return FALSE;
                }
                usleep(10000);
                continue;
            }
            return FALSE;
        }
        else if (li_Nread == 0) 
        {
            return FALSE;                   /* Socket close? */
        }

        li_Nleft -= li_Nread;
        pp_Ptr   += li_Nread;
    }
    *pi_Nrecv = pi_Nbytes - li_Nleft;
    return TRUE;
}

extern int BS_NetworkWriteN(int pi_Fd, char *pp_Ptr, int pi_Nbytes, int *pi_Nsend)
/**************************************************************************************************/
/* DESC    : BS_NetworkWriteN                                                                     */
/* IN_ARG  : fd : write socket fd                                                                 */
/*         : ptr : send buffer                                                                    */
/*         : nbytes : write 할 전체 length                                                        */
/* OUT_ARG : nsend : send length                                                                  */
/*                   nbytes = nsend : 정상                                                        */
/*                   nbytes > nsend : 오류 (송신중 오류발생, buffer full or socket close)         */
/*                   nrecv  = 0     : 오류 (socket close)                                         */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    int li_N, li_Err, li_Nleft;
    int li_Nwritten, li_Retry;

    li_Retry = 0;
    li_Nleft = pi_Nbytes;
    *pi_Nsend = 0;          /* default return value : 0 setting, 오류일때만 0으로 return */
    while(li_Nleft > 0) 
    {
        li_Nwritten = send(pi_Fd, pp_Ptr, li_Nleft, 0);
        if (li_Nwritten < 0) 
        {
            if ((errno == EWOULDBLOCK) || (errno == EAGAIN)) 
            {
                li_Retry++;
                if (li_Retry > 50) 
                {
                    return FALSE;
                }
                usleep(10000);
                continue;
            }
            return FALSE;
        } 
        else if (li_Nwritten == 0) 
        {
            return FALSE;                   /* Socket close? */
        }

        li_Nleft -= li_Nwritten;
        pp_Ptr   += li_Nwritten;
    }

    *pi_Nsend = pi_Nbytes - li_Nleft;
    return TRUE;
}

extern int XD_Tsyslog(const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : BS_Tsyslog (향후 Std SYSLOG로 교체될 임시 module)                                    */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		log_file[100];
    long		file_size;

    if ((lp_fp = fopen(XD_LOG_FILE, "a")) == NULL)
    {
        printf("Log file open error\n");
        return FALSE;
    }

    time(&lt_time);
    lt_today = localtime(&lt_time);

    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    fprintf(lp_fp,     "[%04d-%02d-%02d %02d:%02d:%02d]%s",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            lx_buffer);
	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
	if (file_size > max_log_file_size)
	{
		sprintf(log_file, "%s.old", XD_LOG_FILE);
		if (rename(XD_LOG_FILE, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%ld]\n", file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%ld]\n", file_size);			
		}
	}    
    return TRUE;
}

extern int BS_Tsyslog(const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : BS_Tsyslog (향후 Std SYSLOG로 교체될 임시 module)                                    */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		log_file[100];
    long		file_size;


    time(&lt_time);
    lt_today = localtime(&lt_time);

//	sprintf(log_file, "%s-%04d%02d%02d.log", LOG_FILE, lt_today->tm_year + 1900, lt_today->tm_mon + 1, lt_today->tm_mday);
//    if ((lp_fp = fopen(log_file, "a")) == NULL)
    if ((lp_fp = fopen(LOG_FILE, "a")) == NULL)
    {
        printf("Log file open error\n");
        return FALSE;
    }

    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    fprintf(lp_fp,     "[%04d-%02d-%02d %02d:%02d:%02d]%s",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            lx_buffer);

	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
	if (file_size > max_log_file_size)
	{
		sprintf(log_file, "%s.old", LOG_FILE);
		if (rename(LOG_FILE, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%s][%ld]\n", LOG_FILE, file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%s][%ld]\n", LOG_FILE, file_size);		
		}
	}    
    return TRUE;
}

extern int BS_Tsyslog2(const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : BS_Tsyslog (향후 Std SYSLOG로 교체될 임시 module)                                    */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		log_file[100];
    long		file_size;


    time(&lt_time);
    lt_today = localtime(&lt_time);

//	sprintf(log_file, "%s-%04d%02d%02d.log", LOG_FILE, lt_today->tm_year + 1900, lt_today->tm_mon + 1, lt_today->tm_mday);
//    if ((lp_fp = fopen(log_file, "a")) == NULL)
    if ((lp_fp = fopen(LOG_FILE2, "a")) == NULL)
    {
        printf("Log file open error\n");
        return FALSE;
    }

    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    fprintf(lp_fp,     "[%04d-%02d-%02d %02d:%02d:%02d]%s",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            lx_buffer);

	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
	if (file_size > max_log_file_size*5)
	{
		sprintf(LOG_FILE2, "%s.old", LOG_FILE2);
		if (rename(LOG_FILE2, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%s][%ld]\n", LOG_FILE2, file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%s][%ld]\n", LOG_FILE2, file_size);		
		}
	}    
    return TRUE;
}

extern int BACKUP_LOG(char backup_step, char *vm_uuid, char *file_name, const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : BACKUP_LOG 										                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		backup_text[100];
    char		log_file[100];
    long		file_size;
    
    switch(backup_step)
    {
    	case	'S' :	strcpy(backup_text,		"SNAPSHOT create");
    		            break;	
    	case	'P' :	strcpy(backup_text,		"SNAPSHOT VM property Change");
    		            break;	
    	case	'E' :	strcpy(backup_text,		"VM BACKUP");	// export
    		            break;	
    	case	'D' :	strcpy(backup_text,		"SNAPSHOT destroy");
    		            break;	
    	case	'I' :	strcpy(backup_text,		"VM IMPORT");
    		            break;	
    	case	'R' :	strcpy(backup_text,		"Partial VM BACKUP");
    		            break;	
		default		:	BS_Tsyslog("BACKUP LOG parameter invalid.  Skip this request.[%c]\n", backup_step);
						return FALSE;
    }

    if ((lp_fp = fopen(BACKUP_LOG_FILE, "a")) == NULL)
    {
        BS_Tsyslog("Xenserver BACKUP Log file open error\n");
        return FALSE;
    }

    time(&lt_time);
    lt_today = localtime(&lt_time);

    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    fprintf(lp_fp,     "[%04d-%02d-%02d %02d:%02d:%02d]%s: VM-UUID=%s filename=%s\n\t%s\n",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            backup_text, vm_uuid, file_name,
                            lx_buffer);
	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
	if (file_size > max_log_file_size)
	{
		sprintf(log_file, "%s.old", BACKUP_LOG_FILE);
		if (rename(BACKUP_LOG_FILE, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%ld]\n", file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%ld]\n", file_size);			
		}
	}    
    return TRUE;
}

extern int IMPORT_LOG(char backup_step, char *sr_uuid, char *file_name, const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : BACKUP_LOG 										                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		backup_text[100];
    char		log_file[100];
    long		file_size;
        
    switch(backup_step)
    {
    	case	'I' :	strcpy(backup_text,		"VM IMPORT");
    		            break;	
		default		:	BS_Tsyslog("IMPORT LOG parameter invalid.  Skip this request.[%c]\n", backup_step);
						return FALSE;
    }

    if ((lp_fp = fopen(BACKUP_LOG_FILE, "a")) == NULL)
    {
        BS_Tsyslog("Xenserver BACKUP Log file open error\n");
        return FALSE;
    }

    time(&lt_time);
    lt_today = localtime(&lt_time);

    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    fprintf(lp_fp,     "[%04d-%02d-%02d %02d:%02d:%02d]%s: SR-UUID=%s filename=%s\n\t%s\n",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            backup_text, sr_uuid, file_name,
                            lx_buffer);
	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
	if (file_size > max_log_file_size)
	{
		sprintf(log_file, "%s.old", BACKUP_LOG_FILE);
		if (rename(BACKUP_LOG_FILE, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%ld]\n", file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%ld]\n", file_size);			
		}
	}    
    return TRUE;
}

// 향후 log file 분리시 log seq의 년월을 기준으로 파일을 찾을 것    
// TRAN_LOG2('5', "SYNC Queue result send error", "%s", msg.msg_text);
// TRAN_LOG2로 전체 전환되어 아래 함수는 사용 중지
extern int TRAN_LOG(char log_type, char *log_text, const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : TRAN_LOG 																			  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		buffer[18];
    struct		timeval	tv;
    char		log_file[100];
    long		file_size;

    if ((lp_fp = fopen(TRAN_LOG_FILE, "a")) == NULL)
    {
        BS_Tsyslog("Transaction Log file open error\n");
        return FALSE;
    }

    time(&lt_time);
    lt_today = localtime(&lt_time);
	gettimeofday(&tv, NULL);
    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    memset(buffer, 0x00, sizeof(buffer));
    memcpy(buffer, &lx_buffer[8], 17);
    fprintf(lp_fp,     "%c[%04d-%02d-%02d %02d:%02d:%02d:%03ld]%s %c:%s\n%c%s%c\n",
							0x00,
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            tv.tv_usec/1000,
                            buffer,
                            log_type,
                            log_text,
                            0x00,
                            lx_buffer,
                            0x00);
	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
	if (file_size > max_log_file_size)
	{
		sprintf(log_file, "%s.old", TRAN_LOG_FILE);
		if (rename(TRAN_LOG_FILE, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%ld]\n", file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%ld]\n", file_size);			
		}
	}    
    return TRUE;
}

// 향후 log file 분리시 log seq의 년월을 기준으로 파일을 찾을 것    
// TRAN_LOG2('5', "SYNC Queue result send error", "%s", msg.msg_text);
extern int TRAN_LOG2(char log_type, char *log_text, const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : TRAN_LOG 																			  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		buffer[18];
    struct		timeval	tv;
    char		log_file[100];
    long		file_size;

    time(&lt_time);
    lt_today = localtime(&lt_time);
	gettimeofday(&tv, NULL);

    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    memset(buffer, 0x00, sizeof(buffer));
    memcpy(buffer, &lx_buffer[1], 17);

	sprintf(log_file, "%s-%.6s.log", TRAN_LOG_FILE, buffer);
//    if ((lp_fp = fopen(TRAN_LOG_FILE, "a")) == NULL)
    if ((lp_fp = fopen(log_file, "a")) == NULL)
    {
        BS_Tsyslog("Transaction Log file open error\n");
        return FALSE;
    }

    fprintf(lp_fp,     "%c[%04d-%02d-%02d %02d:%02d:%02d:%03ld]%s %c:%s\n%c%s%c\n",
    						0x00,
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            tv.tv_usec/1000,
                            buffer,
                            log_type,
                            log_text,
                            0x00,
                            lx_buffer,
                            0x00);
	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
    // 전문 조회를 위해 
    /*
	if (file_size > max_log_file_size)
	{
		sprintf(log_file, "%s.old", TRAN_LOG_FILE);
		if (rename(TRAN_LOG_FILE, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%ld]\n", file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%ld]\n", file_size);			
		}
	}
	*/   
    return TRUE;
}

// for cifs vbr manager
extern int TRAN_LOG3(const char *pp_Format, ...)
/**************************************************************************************************/
/* DESC    : TRAN_LOG 																			  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    va_list     lp_args;
    char        lx_buffer[MAX_BUFFER_SIZE];
    FILE        *lp_fp;
    struct tm*  lt_today;
    time_t      lt_time;
    char		buffer[18];
    struct		timeval	tv;
    char		log_file[100];
    long		file_size;

    time(&lt_time);
    lt_today = localtime(&lt_time);
	gettimeofday(&tv, NULL);

    va_start(lp_args, pp_Format);
    vsprintf(lx_buffer, pp_Format, lp_args);
    if ((lp_fp = fopen(TRAN_LOG_FILE2, "a")) == NULL)
    {
        BS_Tsyslog("Transaction Log file open error\n");
        return FALSE;
    }

    fprintf(lp_fp,     "%c[%04d-%02d-%02d %02d:%02d:%02d:%03ld]%s\n",
    						0x00,
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec,
                            tv.tv_usec/1000,                            
                            lx_buffer);
	file_size	=	ftell(lp_fp);
    fclose(lp_fp);
	if (file_size > max_log_file_size)
	{
		sprintf(log_file, "%s.old", TRAN_LOG_FILE2);
		if (rename(TRAN_LOG_FILE2, log_file) == 0)
		{
			BS_Tsyslog("LOG FILE switch success : before file size [%s][%ld]\n", TRAN_LOG_FILE2, file_size);
		}
		else
		{
			BS_Tsyslog("LOG FILE switch failed : current file size [%s][%ld]\n", TRAN_LOG_FILE2, file_size);		
		}
	}    
    return TRUE;
}

extern int BS_date_time(char *date_time)
{
    struct tm*  lt_today;
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
}

extern int BS_date_time_no_delimiter(char *date_time)
{
    struct tm*  lt_today;
    time_t      lt_time;

    time(&lt_time);
    lt_today = localtime(&lt_time);

    sprintf(date_time,     "%04d%02d%02d-%02d%02d%02d",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec);
}

extern int BS_date_time_no_delimiter2(char *date_time)
{
    struct tm*  lt_today;
    time_t      lt_time;

    time(&lt_time);
    lt_today = localtime(&lt_time);

    sprintf(date_time,     "%04d%02d%02d%02d%02d%02d",
                            lt_today->tm_year + 1900,
                            lt_today->tm_mon + 1,
                            lt_today->tm_mday,
                            lt_today->tm_hour,
                            lt_today->tm_min,
                            lt_today->tm_sec);
}

extern int BS_GetIpByName(char *pp_Host, struct sockaddr_in *pt_InAddr)
/**************************************************************************************************/
/* DESC    : GetIpByName                                                                          */
/* IN_ARG  : host (접속할 hostname 또는 xxx.xxx.xxx.xxx)                                          */
/* OUT_ARG : structure sockaddr_in (ip address)                                                   */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           정상 = TRUE                                                                          */
/*           오류 = FALSE                                                                         */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    struct hostent *lt_Hostent;
    struct sockaddr_in lt_Server;
    
    memset(&lt_Server, 0, sizeof(lt_Server));
    
    if (!inet_aton(pp_Host, &lt_Server.sin_addr)) {
          if ((lt_Hostent = gethostbyname(trim(pp_Host)))) {
             memcpy(&lt_Server.sin_addr.s_addr, lt_Hostent->h_addr_list[0], lt_Hostent->h_length);
          }
          else {
             BS_Tsyslog("Host name or address is not valid.\n");
             return FALSE;
          }
    }   
    memcpy(&(pt_InAddr->sin_addr), &lt_Server.sin_addr, sizeof(struct in_addr));
    BS_Tsyslog("[%s]Host ip = [%s][%s]\n", pp_Host, inet_ntoa(lt_Server.sin_addr), inet_ntoa(pt_InAddr->sin_addr));
    return TRUE;
}

extern int BS_Daemonize()
/**************************************************************************************************/
/* DESC    : BS_Daemonize                                                                         */
/* IN_ARG  : N/A                                                                                  */
/* OUT_ARG : N/A                                                                                  */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           TRUE  = 정상                                                                         */
/*           FALSE = 오류                                                                         */
/* REMARKS : 오류 발생시 system process full등의 원인이므로 system admin 확인이 필요              */
/**************************************************************************************************/
{
    int    li_result;
    if ((li_result = fork()) < 0)
    {
        BS_Tsyslog("FATAL Error : Log Inquiry process is not changed(DAEMON).\n");
        return FALSE;
    }
    
    if (li_result > 0)      /* Parent process coming down.... */
    {
        exit(0);
    }
    
    if ((li_result = setsid()) < 0)
    {
        BS_Tsyslog("FATAL Error : Log Inquiry process is not changed(SETSID).\n");
        return FALSE;       
    }
    umask(0);
    
    return TRUE;    
}

extern int BS_GetProfileString(char *pp_App, char *pp_Key, char *pp_Ret, int pi_Size)
/**************************************************************************************************/
/* DESC    : GetProfileString                                                                     */
/* IN_ARG  : app : config file에서 찾을 Main session key                                          */
/*           key : sub session key                                                                */
/*           size: 정상 검색된 경우 결과 값의 최대 길이                                           */
/* OUT_ARG : Ret : Main / Sub session에서 검색된 key value                                        */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           정상 = TRUE                                                                          */
/*           오류 = FALSE                                                                         */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
    char lx_BufKey[MAX_LENGTH] ="";
    char lx_BufValue[MAX_LENGTH] ="";
    char *lp_P=0;
    int li_Bingo;
    FILE *lp_Fp;

    if ((lp_Fp=fopen(CONFIG_FILE, "r")) == 0 )
    {
        BS_Tsyslog("FATAL ERROR : Config FILE open error [%s]\n", CONFIG_FILE );
        return FALSE;
    }

    // input parameter invalid
    if( (strlen(pp_Key) < 1) || strlen(pp_App) < 1)
    {
        BS_Tsyslog("FATAL ERROR : Search text is not valid. [%s][%s]\n", pp_App, pp_Key );
        fclose(lp_Fp);
        return FALSE;
    }
    
    // file handle error
    if(lp_Fp == 0)
    {
        BS_Tsyslog("FATAL ERROR : Config file handle failed.\n" );
        return FALSE;
    }

    // move file start point
    fseek(lp_Fp, 0L, SEEK_SET);   
    if ( seekApp(lp_Fp, pp_App) != 0 )
    {
        BS_Tsyslog("FATAL ERROR : config file search error.. Please check file[%s][%s][%s].\n", CONFIG_FILE, pp_App, pp_Key );
        fclose(lp_Fp);
        return FALSE;
    }

    // find key
    getNextKey(lp_Fp, lx_BufKey, sizeof(lx_BufKey), lx_BufValue, sizeof(lx_BufValue) );
    li_Bingo=0;

    while( lx_BufKey[0] )
    {
        if ( strcmp(lx_BufKey, pp_Key) == 0 )
        {
            strncpy( pp_Ret, lx_BufValue, pi_Size );
            pp_Ret[pi_Size-1]=0;
            li_Bingo=1;
            break;
        }       
        getNextKey(lp_Fp, lx_BufKey, sizeof(lx_BufKey), lx_BufValue, sizeof(lx_BufValue) );
    }
    
    // no key found
    if (li_Bingo==0)
    {
        // BS_Tsyslog("FATAL ERROR : Not found search text.[%s][%s]\n", pp_App, pp_Key);
        fclose(lp_Fp);
        return FALSE;
    }
    fclose(lp_Fp);
    return TRUE;
}
// 현재 fp 부터 검색한다.
// app 위치로 이동한다.
int seekApp(FILE *fp, char *app)
{
    char str[MAX_LENGTH] ="";
    char buf[MAX_LENGTH] ="";
    int bingo;

    // 파라이터 오류
    if(strlen(app) < 1) return -1;

    // 파일 핸들 오류
    if(fp == 0) return -2;

    sprintf(str, "[%s]", app);

    // find app
    bingo=0;
    while( feof(fp) == 0 )
    {
        fgets(buf, MAX_LENGTH, fp);

        if (buf[0] == 0 || buf[0] == '#') continue;

        trim(buf);
        if (strncmp(buf, str, strlen(str)) == 0)
        {
            bingo=1;
            break;
        }
    }
    // app 없음
    if (bingo==0) return -3;
    
    return 0;
}

// 현재 fp 부터 검색한다.
// 다음 key 값을 얻는다.
void getNextKey(FILE *fp, char *retKey, int sizeKey, char *retValue, int sizeValue)
{
    char str[MAX_LENGTH] ="";
    char buf[MAX_LENGTH] ="";
    char *p=0;
    int bingo;

    // 파라미터 오류
    if( retKey==0 || retValue==0 ) return;

    // 파일 핸들 오류
    if(fp == 0) return;

    // find key
    bingo=0;
    while( feof(fp) == 0 )
    {
        fgets(buf, MAX_LENGTH, fp);

        trim(buf);
        if (buf[0] == 0 || buf[0] == '#') continue;
        if (buf[0] == '[' ) return;
        
        break;
    }
    
    // 토큰을 찾는다.
    p = buf;
    while( (*p != '\n') && (*p != '=') && (*p) ) p++;
    // value가 있다.
    if ( *p == '=' ) bingo = 1;
    *p = 0;
    
    // key!!
    strncpy(retKey, trim(buf), sizeKey);
    retKey[sizeKey-1] = 0;

    // value        
    retValue[0] = 0;
    if (bingo == 1)
    {
        strcpy(str, p+1);
        
        // 토큰을 찾는다.
        p = str;
        while( (*p != '\n') && (*p) ) p++;
        *p = 0;
        
        // value!!
        strncpy( retValue, trim(str), sizeValue);
        retValue[sizeValue-1] = 0;
    }
  
    return;    
}


// fp파일에서 [PROC] app를 찾아 key 갯수를 센다.
int GetKeyCount(FILE *fp, char *app)
{
    char buf[MAX_LENGTH] ="";
    int count=0;
    
    // 파일 핸들 오류
    if(fp == 0) return -2;    

    // 파일의 맨처음으로 이동
    fseek(fp, 0L, SEEK_SET);
    
    if ( seekApp(fp, app) != 0 ) return -3;

    // count key
    count=0;
    while( feof(fp) == 0 )
    {
        fgets(buf, MAX_LENGTH, fp);

        trim(buf);
        if (buf[0] == '[') break;
        if (buf[0] == 0 || buf[0] == '#') continue;
        count++;
    }
    
    return count;    
}

int isBlank(char ch)
{
    return ( (ch==0x20) || (ch=='\t') || (ch=='\r') || (ch=='\n') );
}

// 양옆 공백 제거후 리턴
// !원본 문자열을 변경 한다.
char* trim( char *s )
{
    char *f=s, *e=0, *c=s;

    /* 뒤쪽 공백 제거 */
    e=s +(strlen(s)) -1;
    while( isBlank(*e) && s<=e) e--;
    *(e+1)=0;

    /* 앞쪽 공백 제거 */
    while( isBlank(*f) && f<=e) f++;

    /* 공백 없는 부분 복사 */
    if (s!=f) {
        while(f<=e) *(c++) = *(f++);
        *c=0;
    }

    return s;
}

extern int HexPrint( char *psp_PgmId,  int pi_Line,  char *psp_MsgId,  char *x_char, int x_len)
/* @ Func Arg  : psp_PgmId : 로그생성 파일명
 * @             pi_Line   : 로그생성 라인수
 * @             psp_MsgId : 로그를 grep으로 추출시 필요한 String(8자이내)
 * @             x_char    : 출력대상 데이터
 * @             x_len     : 출력대상 데이터 길이
 * @ Return    : void : 해당사항없음
 * @ Sample    : char ax_Src[20] = {'0','1','2','3','4','5','6','7','8','9',
 * @                                '0','1','2','3','4','5','6','7','8','9'}
 * @             HexPrint( __FILE__, __LINE__, "CHECK1", ax_Src,
 * @                         sizeof(ax_Src))
 * @ CHECK1  |00000:30 31 32 33 34 35 36 37 38 39 - 30 31 32 33 34 35 36 37 38 39 |01234567890123456789|
!*/
{
#ifdef  NO_PRN_LOG
        return TRUE;
#endif
 
    int     i,j,k,p;
    int     isHangle = 0;
    char    lx_PrtSendYn               [     1];
    char    prt_buf                    [    83];
    char    tmp                        [    30];
    char    lx_NULL                    [    62] ="00 00 00 00 00 00 00 00 00 00 - 00 00 00 00 00 00 00 00 00 00";
    char    lx_SPACE                   [    62] ="20 20 20 20 20 20 20 20 20 20 - 20 20 20 20 20 20 20 20 20 20";
 		char		lx_PrintBuff[1024];
    lx_PrtSendYn[0]='N';
 
    for (i = 0; i < x_len; i+=20)
    {
        k = i + 20;
        if (x_len < 20)   k = x_len;
        else
        if (k > x_len)    k =  x_len + 20 - k;
        else              k = 20;
 
        memset(prt_buf,0x20,sizeof(prt_buf));
        prt_buf[30] = '-';
 
        for (j = 0; j < k; j++)
        {
            sprintf(tmp,"%02X ",x_char[i+j]);
            if (j < 10)    memcpy(&prt_buf[j*3  ],tmp,3);
            else           memcpy(&prt_buf[j*3+2],tmp,3);
        }
 
        // NULL/SPACE 출력되는 line은 skip : 20100402
        if ((memcmp(lx_NULL, prt_buf, strlen(lx_NULL)) == 0) || (memcmp(lx_SPACE, prt_buf, strlen(lx_SPACE)) == 0 ))
        {
            continue;
        }
 
        prt_buf[62] = '|';
 
        for (j = 0; j < k; j++)
        {
            if (x_char[i+j] < 0x20 )
            {
                prt_buf[63 + j] = '.';
                continue;
            }
            if ((x_char[i+j]&0x80) > 0)
            {
                if (isHangle == 0)
                {
                    if (j == 19)    prt_buf[63 + j] = '?';
                    else            prt_buf[63 + j] = x_char[i+j];
                    isHangle = 1;
                }
                else
                {
                    if (j == 0)
                    {
                        if (isHangle == 1)   prt_buf[63 + j] = '?';
                        else                 prt_buf[63 + j] = x_char[i+j];
                    }
                    else
                    {
                        prt_buf[63 + j] = x_char[i+j];
                    }
                    isHangle = 0;
                }
            }
            else
            {
                prt_buf[63 + j] = x_char[i+j];
            }
        }
        sprintf(lx_PrintBuff,"%05d:%.*s",i,sizeof(prt_buf),prt_buf);
 
        BS_Tsyslog("%s|}\n", lx_PrintBuff) ;
 
    }  /* end of for */
 
    return TRUE;
}

extern char *ethernet_mactoa(struct sockaddr *addr) 
{ 
	static char buff[256]; 
		
		unsigned char *ptr = (unsigned char *) addr->sa_data;
	
		sprintf(buff, "%02X:%02X:%02X:%02X:%02X:%02X", 
			(ptr[0] & 0xff), (ptr[1] & 0xff), (ptr[2] & 0xff), 
			(ptr[3] & 0xff), (ptr[4] & 0xff), (ptr[5] & 0xff)); 
	
	return (buff);
} 

extern char BS_initialize()
/**************************************************************************************************/
/* DESC    : BS_initialize                                		                                  */
/* IN_ARG  : 				              				                                          */
/* OUT_ARG :			                                                                          */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
	max_log_file_size	=	50000000L;		// default setting
}

extern char *BS_EthernetMac(char *ipaddress, char *nic)
/**************************************************************************************************/
/* DESC    : BS_EthernetMac                                		                                  */
/* IN_ARG  : ipaddress					                                                          */
/* OUT_ARG : mac_address                                                                          */
/* RETURN  : 처리결과의 정상/오류여부를 리턴한다                                                  */
/*           return TRUE  = 정상                                                                  */
/*           return FALSE = 오류                                                                  */
/* REMARKS :                                                                                      */
/**************************************************************************************************/
{
	int                 s;
	struct arpreq       areq;
	struct sockaddr_in *sin;
	struct in_addr      ipaddr;

	/* Get an internet domain socket. 
	*/
	if ((s = socket(AF_INET, SOCK_DGRAM, 0)) == -1) {
		BS_Tsyslog("socket internal error");
		exit(1);
	}
	
	/* Make the ARP request. 
	*/
	memset(&areq, 0, sizeof(areq));
	sin = (struct sockaddr_in *) &areq.arp_pa;
	sin->sin_family = AF_INET;

	if (inet_aton(ipaddress, &ipaddr) == 0) {
		BS_Tsyslog("-- Error: bad dotted-decimal IP '%s'.\n", ipaddress);
		return NULL;
	}
	
	sin->sin_addr = ipaddr;
	sin = (struct sockaddr_in *) &areq.arp_ha;
	sin->sin_family = ARPHRD_ETHER;

	strncpy(areq.arp_dev, nic, 15);
	BS_Tsyslog("5\n");
	
	if (ioctl(s, SIOCGARP, (caddr_t) &areq) == -1) {
		BS_Tsyslog("-- Error: unable to make ARP request, error");
		return NULL;
	}

//	BS_Tsyslog("%s (%s) -> %s\n", ipaddress, inet_ntoa(&((struct sockaddr_in *) &areq.arp_pa)->sin_addr), ethernet_mactoa(&areq.arp_ha));
	BS_Tsyslog("%s -> %s\n", ipaddress, ethernet_mactoa(&areq.arp_ha));
    return ethernet_mactoa(&areq.arp_ha);
}


int text_cut(char *msg, char *msg_out, char delimiter, int max_length)
{
	int index;
	
	for (index = 0; index < max_length; index++)
	{
		if (*(msg+index) == 0x00)
		{
			memcpy(msg_out, msg, index);
			break;
		}	
	}
}
// 원본 문자열 a, 검색 문자열 text, 결과 문자열 result
// 검색 후 ';' or NULL이 나올때까지 검색 후 결과에 저장
int find_str(char *a, char *text, char *result)
{
	char	*p;
	char	*value;
	int		cnt;
	
	cnt = 0;	
	p = strstr(a, text);
	if (p == NULL)
	{
		printf("Not found.\n");
		return FALSE;
	}
	
	p += strlen(text);
	printf("=== [%s][%d]\n", p,  strlen(text));
	value = p;
	
	while (1)
	{
		if ((*p == ';') || (*p == 0x00))
			break;
		cnt++; p++;
	}
	memcpy(result, value, cnt);
	result[cnt] = 0x00;
	return TRUE;
}

// 원본 문자열 처음부터 LF 검색후 msg2에 문자열 복제
// 원본 문자열은 복제된 부분을 제외하고 저장됨
// usage #1
//	result = 0;	
//	do
//	{
//		memset(buffer, 0x00, sizeof(buffer));
//		result = msg_cut(a, buffer);
//		printf("buffer = [%d][%s][%s]\n", result, buffer, a);	
//	} while(result);
//	usage #2
//	while ((result = msg_cut(a, buffer)) == TRUE)
//	{
//		printf("buffer2 = [%d][%s][%s]\n", result, buffer, a);			
//		memset(buffer, 0x00, sizeof(buffer));
//	}

int msg_cut (char *msg, char *msg2)
{
	int i;
	char	*p;
	char	a[MAX_BUFFER_SIZE];
	strcpy(a, msg);
	p = msg;
	
	i = 0;
	while (1)
	{
		if (*p == 0x0A)
		{
			memcpy(msg2, msg, i);
			strcpy(msg, &a[i+1]);
//			BS_Tsyslog("=======DEBUG:[%s][%s]============\n", msg2, msg);
			return TRUE;
		}
		
		if (*p == 0x00)
		{
			memcpy(msg2, msg, i);
			return FALSE;
		}
		i++; p++;
	}
}
