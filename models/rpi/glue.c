#include "glue.h"
#include <wiringx.h>
#include <pthread.h>
#include "_cgo_export.h"

// CAN_RX is on BCM GPIO 25 (aka WiringPin 6)
// CAN_TX is on BCM GPIO 22 (aka WiringPin 3)
// MCU_RESET is on BCM GPIO 26 (aka WiringPin 25)

#define CAN_RX_PIN 25
#define CAN_TX_PIN 22
#define MCU_RESET_PIN 26 
#define SPI_CE0 8

int digitalReadRx(void)
{
    return digitalRead(CAN_RX_PIN);
}

int digitalReadTx(void)
{
    return digitalRead(CAN_TX_PIN);
}

int digitalReadCE0(void)
{
    return digitalRead(SPI_CE0);
}

void setup_wiring_pi(void)
{
    wiringXSetup("rock4", NULL);
    pinMode(CAN_RX_PIN, PINMODE_INPUT | PINMODE_INTERRUPT);
    pinMode(CAN_TX_PIN, PINMODE_INPUT);
    pinMode(MCU_RESET_PIN, PINMODE_INPUT); // avoid leaving the MCU stuck at reset
    // pullUpDnControl(CAN_TX_PIN, PUD_DOWN);
}

void *interruptHandler() {
	for (;;)
    {
        if (waitForInterrupt(CAN_RX_PIN, -1) > 0)
        {
            CanRxInterrupt();
        }
    }

    return NULL;
}

static pthread_mutex_t pinMutex;
void setup_interrupts()
{
    pthread_t threadId;

    pthread_mutex_lock (&pinMutex) ;
    pthread_create (&threadId, NULL, interruptHandler, NULL);
    pthread_mutex_unlock (&pinMutex) ;    
    wiringXISR(CAN_RX_PIN, ISR_MODE_FALLING);
    //wiringPiISR(CAN_TX_PIN, INT_EDGE_FALLING, CanTxInterrupt);
}
