#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <signal.h>
#include <fcntl.h>
#include <arpa/inet.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <stdarg.h>
#include <time.h>
#include <errno.h>
#include <netdb.h>
#include <time.h>
#include <sys/ipc.h>
#include <sys/msg.h>
#include <sys/shm.h>
#include <net/if_arp.h>
#include <linux/sockios.h>
#include <pthread.h>
#include <sys/io.h>
#include <dirent.h>
#include <zlib.h>

#define TRUE    			1
#define FALSE   			0
#define EXIST   			2
#define	CHANGE				9
#define RETURN_TRUE     	return TRUE
#define RETURN_FALSE    	return FALSE
                        	
#define LOG_FILE			"./server.log"
#define TRAN_LOG_FILE		"./transaction"
#define TRAN_LOG_FILE2		"./transaction.log"
#define XD_LOG_FILE			"./client_connect.log"
#define CONFIG_FILE			"./node_info.cfg"
#define BACKUP_LOG_FILE		"./backup.log"
#define SEQUENCE_FILE		"./sequence.log"
#define	XENSERVER_INVENTORY			"/etc/xensource-inventory"
                        	
#define MAX_LENGTH 			256
#define MAX_DATA_SIZE   	300000
#define MAX_BUFFER_SIZE 	300000
#define MAX_SEND_SIZE   	300000
#define	MAX_RECV_SIZE		300000
#define MAX_CLIENT_CNT  	100
#define MAX_STRING_LEN  	256
#define	MAX_QUEUE_LEN		300000
                       	
#define	MAX_XEN_CNT			100
#define	MAX_VM_CNT			10000
#define	MAX_VDI_CNT			10000
                        	
#define	RETRY_SECOND		10
#define	MAX_LOCAL_VM_CNT	10
#define WAIT_TIME       	3

// CIFS 관련 define
#define CIFS_BUFFER			640000
#define	MAX_CIFS_THREAD		20
#define	MAX_FILE_LEN		1024

int		si_PortNo;					/* Listen tcp port no */
int		si_ServSock;				/* Listen socket fd */
int		si_ClntSock;				/* client connect socket fd */
int		si_CurrFd;					/* current connect socket fd */
int		si_FdMax;					/* Max select fd */
char	sx_Message[MAX_BUFFER_SIZE];/* received message buffer variable */
fd_set 	s_reads, s_temps;
long	max_log_file_size;			// max log file size
// Socket connection FD
typedef struct  {
    	int flag;
    	int fd;
    	char ip_address[20];
} FD_INFO;

/* Standard API */
extern int BS_SubLog(const char *, const char *, const int, struct timeval *, struct timeval *, int );
extern int BS_Tsyslog(const char *pp_Format, ...);
extern int TRAN_LOG(char, char *, const char *pp_Format, ...);
extern int XD_Tsyslog(const char *pp_Format, ...);
extern int BS_NetworkListen(char *, char *);
extern int BS_NetworkAccept(char *ipaddress);
extern int BS_NetworkConnect(char *pp_Host, unsigned int pi_Ip, int pi_PortNo, int *pp_Fd);
extern int BS_NetworkReadN(int pi_Fd, char *pp_Ptr, int pi_Nbytes, int *pi_NRecv);
extern int BS_NetworkWriteN(int pi_Fd, char *pp_Ptr, int pi_Nbytes, int *pi_NSend);
extern int BS_GetIpByName(char *pp_Host, struct sockaddr_in *);
extern char *BS_EthernetMac(char *ipaddress, char *nic);
static char *ethernet_mactoa(struct sockaddr *addr);
extern int BACKUP_LOG(char backup_type, char *vm_uuid, char *file_name, const char *pp_Format, ...);

/* Non-standard API : 향후 표준 함수로 변경 예정 */
int BS_GetProfileString(char *app, char *key, char *ret, int size);
int GetKeyCount(FILE *fp, char *app);
int seekApp(FILE *fp, char *app);
void getNextKey(FILE *fp, char *retKey, int sizeKey, char *retValue, int sizeValue);
int isBlank(char ch);
char* trim( char *s );
int check_client(int sock, char *ip_address);
void *msg_receiver(void *args);
int remove_client(int fd);
int text_cut(char *msg, char *msg_out, char delimiter, int max_length);


