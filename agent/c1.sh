cc -g -c bs_common.c -o bs_common.o
#cc -g -O $1.c bs_common.o -o $1 -lpthread -D_REENTRANT -lm -L /usr/lib/mysql -lmysqlclient
cc -g -O $1.c bs_common.o -o $1 -lpthread -D_REENTRANT -lm -lz
cp -p $1 /tmp
